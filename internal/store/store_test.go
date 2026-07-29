package store

import (
	"context"
	"testing"
	"time"

	"github.com/Rehtt/rssdownload/internal/database"
	"github.com/Rehtt/rssdownload/internal/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, _, err := database.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	return New(db)
}

func parsedItem(seed string) domain.ParsedItem {
	published := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	return domain.ParsedItem{
		ItemKey:           "item-" + seed,
		ReleaseKey:        "0123456789abcdef" + seed,
		GUID:              "guid-" + seed,
		Title:             "Release " + seed,
		DetailURL:         "https://mikanani.me/Home/Episode/" + seed,
		EnclosureURL:      "https://mikanani.me/Download/" + seed + ".torrent",
		EnclosureLength:   1024,
		PublishedAt:       &published,
		RecognitionStatus: domain.RecognitionRecognized,
	}
}

func TestInitialBaselineAndSubsequentQueue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataStore := newTestStore(t)
	subscription, err := dataStore.CreateSubscription(
		ctx,
		"Example",
		"https://mikanani.me/RSS/MyBangumi?token=secret",
		"",
		false,
	)
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	if err := dataStore.ApplyFeedSync(
		ctx,
		subscription.ID,
		"Example",
		"etag-1",
		"",
		[]domain.ParsedItem{parsedItem("one")},
		time.Now().Add(10*time.Minute),
	); err != nil {
		t.Fatalf("apply initial feed: %v", err)
	}
	firstPage, err := dataStore.ListItems(ctx, ItemFilter{})
	if err != nil {
		t.Fatalf("list initial items: %v", err)
	}
	if len(firstPage.Items) != 1 || firstPage.Items[0].Download != nil {
		t.Fatalf("initial baseline should not queue downloads: %#v", firstPage.Items)
	}
	if err := dataStore.ApplyFeedSync(
		ctx,
		subscription.ID,
		"Example",
		"etag-2",
		"",
		[]domain.ParsedItem{parsedItem("one"), parsedItem("two")},
		time.Now().Add(10*time.Minute),
	); err != nil {
		t.Fatalf("apply later feed: %v", err)
	}
	secondPage, err := dataStore.ListItems(ctx, ItemFilter{})
	if err != nil {
		t.Fatalf("list updated items: %v", err)
	}
	if secondPage.Total != 2 {
		t.Fatalf("expected two items, got %d", secondPage.Total)
	}
	var queued int
	for _, item := range secondPage.Items {
		if item.Download != nil && item.Download.Status == domain.DownloadQueued {
			queued++
		}
	}
	if queued != 1 {
		t.Fatalf("expected only the new item to queue, got %d", queued)
	}
}

func TestSettingsPersistProxyConfiguration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dataStore := newTestStore(t)
	current, err := dataStore.Settings(ctx)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	current.ProxyURL = "socks5://proxy.example:1080"
	current.ProxyUsername = "proxy-user"
	current.ProxyPassword = "proxy-pass"

	updated, err := dataStore.UpdateSettings(ctx, current)
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if updated.ProxyURL != current.ProxyURL ||
		updated.ProxyUsername != current.ProxyUsername ||
		updated.ProxyPassword != current.ProxyPassword {
		t.Fatalf("proxy settings were not persisted: %#v", updated)
	}
}

func TestInitialDownloadExistingAndArchiveHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataStore := newTestStore(t)
	subscription, err := dataStore.CreateSubscription(
		ctx,
		"Example",
		"https://mikanani.me/RSS/MyBangumi?token=secret",
		"/anime",
		true,
	)
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	if err := dataStore.ApplyFeedSync(
		ctx,
		subscription.ID,
		"Example",
		"",
		"",
		[]domain.ParsedItem{parsedItem("existing")},
		time.Now().Add(10*time.Minute),
	); err != nil {
		t.Fatalf("apply feed: %v", err)
	}
	page, err := dataStore.ListItems(ctx, ItemFilter{})
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Download == nil {
		t.Fatal("initial item should have a download")
	}
	if directory, err := dataStore.DownloadDirectory(ctx, page.Items[0].Download.ID); err != nil || directory != "/anime" {
		t.Fatalf("unexpected download directory %q, %v", directory, err)
	}
	if err := dataStore.ArchiveSubscription(ctx, subscription.ID); err != nil {
		t.Fatalf("archive subscription: %v", err)
	}
	active, err := dataStore.ListSubscriptions(ctx, false)
	if err != nil {
		t.Fatalf("list active subscriptions: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("archived subscription is still active: %#v", active)
	}
	history, err := dataStore.ListItems(ctx, ItemFilter{})
	if err != nil || history.Total != 1 {
		t.Fatalf("history was not retained: total=%d err=%v", history.Total, err)
	}
}
