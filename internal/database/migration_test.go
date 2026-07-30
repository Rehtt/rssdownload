package database_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Rehtt/rssdownload/internal/database"
	"github.com/Rehtt/rssdownload/internal/domain"
	"github.com/Rehtt/rssdownload/internal/store"
	gormsqlite "github.com/libtnb/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestOpenMigratesLegacyDatabaseWithoutLosingData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "rssdownload.db")
	legacy, err := gorm.Open(gormsqlite.Open(path), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}

	legacyStatements := []string{
		`CREATE TABLE admins (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			admin_id INTEGER NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
			token_hash BLOB NOT NULL UNIQUE,
			csrf_token TEXT NOT NULL,
			created_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			expires_at TEXT NOT NULL
		)`,
		`CREATE TABLE settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			aria2_url TEXT NOT NULL,
			aria2_secret TEXT NOT NULL,
			default_download_dir TEXT NOT NULL,
			poll_interval_minutes INTEGER NOT NULL
				CHECK (poll_interval_minutes BETWEEN 1 AND 1440),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE subscriptions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			download_dir TEXT NOT NULL,
			etag TEXT NOT NULL,
			last_modified TEXT NOT NULL,
			initial_sync_done INTEGER NOT NULL DEFAULT 0,
			initial_download_existing INTEGER NOT NULL DEFAULT 0,
			last_checked_at TEXT,
			next_check_at TEXT,
			last_error TEXT NOT NULL,
			archived_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE downloads (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			release_key TEXT NOT NULL UNIQUE,
			enclosure_url TEXT NOT NULL,
			aria2_gid TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL,
			total_length INTEGER NOT NULL DEFAULT 0,
			completed_length INTEGER NOT NULL DEFAULT 0,
			error_message TEXT NOT NULL,
			retry_count INTEGER NOT NULL DEFAULT 0,
			next_retry_at TEXT,
			last_checked_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE feed_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			subscription_id INTEGER NOT NULL
				REFERENCES subscriptions(id) ON DELETE RESTRICT,
			item_key TEXT NOT NULL,
			guid TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT NOT NULL,
			detail_url TEXT NOT NULL,
			enclosure_url TEXT NOT NULL,
			enclosure_length INTEGER NOT NULL DEFAULT 0,
			published_at TEXT,
			discovered_at TEXT NOT NULL,
			recognition_status TEXT NOT NULL,
			recognition_error TEXT NOT NULL,
			download_id INTEGER REFERENCES downloads(id) ON DELETE SET NULL,
			UNIQUE(subscription_id, item_key)
		)`,
	}
	for _, statement := range legacyStatements {
		if err := legacy.Exec(statement).Error; err != nil {
			t.Fatalf("create legacy schema: %v", err)
		}
	}

	stamp := "2026-07-28T09:10:11.123456789Z"
	tokenHash := []byte("legacy-session-hash")
	inserts := []struct {
		query string
		args  []any
	}{
		{
			`INSERT INTO admins
				(id, username, password_hash, created_at, updated_at)
			 VALUES (1, ?, ?, ?, ?)`,
			[]any{"legacy-admin", "legacy-password-hash", stamp, stamp},
		},
		{
			`INSERT INTO sessions
				(id, admin_id, token_hash, csrf_token, created_at, last_seen_at, expires_at)
			 VALUES (13, 1, ?, ?, ?, ?, ?)`,
			[]any{tokenHash, "legacy-csrf", stamp, stamp, "2099-01-01T00:00:00Z"},
		},
		{
			`INSERT INTO settings
				(id, aria2_url, aria2_secret, default_download_dir,
				 poll_interval_minutes, created_at, updated_at)
			 VALUES (1, ?, ?, ?, ?, ?, ?)`,
			[]any{"http://legacy-aria2:6800/jsonrpc", "legacy-secret", "/legacy", 27, stamp, stamp},
		},
		{
			`INSERT INTO subscriptions
				(id, name, url, enabled, download_dir, etag, last_modified,
				 initial_sync_done, initial_download_existing, last_checked_at,
				 next_check_at, last_error, archived_at, created_at, updated_at)
			 VALUES (7, ?, ?, 1, ?, ?, ?, 1, 0, ?, ?, '', NULL, ?, ?)`,
			[]any{
				"Legacy subscription",
				"https://mikanani.me/RSS/MyBangumi?token=legacy",
				"/legacy/anime",
				`"legacy-etag"`,
				"Mon, 27 Jul 2026 12:00:00 GMT",
				stamp,
				"2026-07-28T09:20:11Z",
				stamp,
				stamp,
			},
		},
		{
			`INSERT INTO downloads
				(id, release_key, enclosure_url, aria2_gid, status, total_length,
				 completed_length, error_message, retry_count, next_retry_at,
				 last_checked_at, created_at, updated_at)
			 VALUES (9, ?, ?, ?, ?, ?, ?, '', 0, NULL, ?, ?, ?)`,
			[]any{
				"legacy-release",
				"https://mikanani.me/Download/legacy.torrent",
				"0123456789abcdef",
				string(domain.DownloadComplete),
				2048,
				2048,
				stamp,
				stamp,
				stamp,
			},
		},
		{
			`INSERT INTO feed_items
				(id, subscription_id, item_key, guid, title, description,
				 detail_url, enclosure_url, enclosure_length, published_at,
				 discovered_at, recognition_status, recognition_error, download_id)
			 VALUES (11, 7, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', 9)`,
			[]any{
				"legacy-item",
				"legacy-guid",
				"Legacy release",
				"Imported from the pre-GORM schema",
				"https://mikanani.me/Home/Episode/legacy",
				"https://mikanani.me/Download/legacy.torrent",
				2048,
				stamp,
				stamp,
				string(domain.RecognitionRecognized),
			},
		},
	}
	for _, insert := range inserts {
		if err := legacy.Exec(insert.query, insert.args...).Error; err != nil {
			t.Fatalf("seed legacy database: %v", err)
		}
	}
	legacySQL, err := legacy.DB()
	if err != nil {
		t.Fatalf("get legacy database pool: %v", err)
	}
	if err := legacySQL.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	db, _, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	dataStore := store.New(db)

	admin, err := dataStore.AdminByUsername(ctx, "legacy-admin")
	if err != nil || admin.PasswordHash != "legacy-password-hash" {
		t.Fatalf("legacy admin was not preserved: %#v, %v", admin, err)
	}
	session, err := dataStore.SessionByTokenHash(ctx, tokenHash)
	if err != nil || session.Username != "legacy-admin" || session.CSRFToken != "legacy-csrf" {
		t.Fatalf("legacy session was not preserved: %#v, %v", session, err)
	}
	settings, err := dataStore.Settings(ctx)
	if err != nil {
		t.Fatalf("read migrated settings: %v", err)
	}
	if settings.Aria2URL != "http://legacy-aria2:6800/jsonrpc" ||
		settings.Aria2Secret != "legacy-secret" ||
		settings.SeedTimeMinutes != nil ||
		settings.SeedRatio != nil ||
		settings.ProxyURL != "" ||
		settings.ProxyUsername != "" ||
		settings.ProxyPassword != "" ||
		settings.DefaultDownloadDir != "/legacy" ||
		settings.PollIntervalMinutes != 27 ||
		!settings.UpdatedAt.Equal(time.Date(2026, 7, 28, 9, 10, 11, 123456789, time.UTC)) {
		t.Fatalf("legacy settings were not preserved: %#v", settings)
	}
	subscriptions, err := dataStore.ListSubscriptions(ctx, false)
	if err != nil || len(subscriptions) != 1 ||
		subscriptions[0].ID != 7 ||
		subscriptions[0].Name != "Legacy subscription" {
		t.Fatalf("legacy subscription was not preserved: %#v, %v", subscriptions, err)
	}
	items, err := dataStore.ListItems(ctx, store.ItemFilter{})
	if err != nil {
		t.Fatalf("read migrated feed items: %v", err)
	}
	if items.Total != 1 ||
		len(items.Items) != 1 ||
		items.Items[0].ID != 11 ||
		items.Items[0].Download == nil ||
		items.Items[0].Download.ID != 9 ||
		items.Items[0].Download.Status != domain.DownloadComplete {
		t.Fatalf("legacy feed/download data was not preserved: %#v", items)
	}
}
