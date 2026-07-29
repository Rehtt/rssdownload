package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/libtnb/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

func Open(ctx context.Context, dataDir string) (*gorm.DB, string, error) {
	absoluteDir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, "", fmt.Errorf("resolve data directory: %w", err)
	}
	if err := os.MkdirAll(absoluteDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("create data directory: %w", err)
	}
	if err := os.Chmod(absoluteDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("protect data directory: %w", err)
	}

	path := filepath.Join(absoluteDir, "rssdownload.db")
	dsn := "file:" + filepath.ToSlash(path) +
		"?_txlock=immediate" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_timezone=UTC"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:         gormlogger.Default.LogMode(gormlogger.Silent),
		TranslateError: true,
		NowFunc:        func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, "", fmt.Errorf("open sqlite with GORM: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, "", fmt.Errorf("get sqlite connection pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(4)
	sqlDB.SetConnMaxLifetime(0)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, "", fmt.Errorf("ping sqlite: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = sqlDB.Close()
		return nil, "", fmt.Errorf("protect sqlite database: %w", err)
	}
	if err := migrate(db.WithContext(ctx)); err != nil {
		_ = sqlDB.Close()
		return nil, "", err
	}
	return db, path, nil
}

func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&AdminModel{},
		&SessionModel{},
		&SettingsModel{},
		&SubscriptionModel{},
		&DownloadModel{},
		&FeedItemModel{},
	); err != nil {
		return fmt.Errorf("auto-migrate database: %w", err)
	}
	now := time.Now().UTC()
	defaults := SettingsModel{
		ID:                  1,
		Aria2URL:            "http://127.0.0.1:6800/jsonrpc",
		Aria2Secret:         "",
		ProxyURL:            "",
		ProxyUsername:       "",
		ProxyPassword:       "",
		DefaultDownloadDir:  "",
		PollIntervalMinutes: 10,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&defaults).Error; err != nil {
		return fmt.Errorf("seed settings: %w", err)
	}
	return nil
}
