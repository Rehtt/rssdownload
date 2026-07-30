package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Rehtt/rssdownload/internal/database"
	"github.com/Rehtt/rssdownload/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db  *gorm.DB
	now func() time.Time
}

func New(db *gorm.DB) *Store {
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
}

type Admin struct {
	ID           int64
	Username     string
	PasswordHash string
}

type Session struct {
	ID        int64
	AdminID   int64
	Username  string
	CSRFToken string
	ExpiresAt time.Time
}

func (s *Store) AdminCount(ctx context.Context) (int, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&database.AdminModel{}).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return int(count), nil
}

func (s *Store) CreateAdmin(ctx context.Context, username, passwordHash string) error {
	now := s.now()
	model := database.AdminModel{
		ID:           1,
		Username:     username,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.db.WithContext(ctx).Create(&model).Error; err != nil {
		return fmt.Errorf("create admin: %w", err)
	}
	return nil
}

func (s *Store) AdminByUsername(ctx context.Context, username string) (Admin, error) {
	var model database.AdminModel
	err := s.db.WithContext(ctx).Where("username = ?", username).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Admin{}, ErrNotFound
	}
	if err != nil {
		return Admin{}, fmt.Errorf("find admin: %w", err)
	}
	return Admin{ID: model.ID, Username: model.Username, PasswordHash: model.PasswordHash}, nil
}

func (s *Store) UpdateAdminPassword(ctx context.Context, username, passwordHash string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&database.AdminModel{}).
			Where("username = ?", username).
			Updates(map[string]any{
				"password_hash": passwordHash,
				"updated_at":    s.now(),
			})
		if result.Error != nil {
			return fmt.Errorf("update admin password: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		if err := tx.Where(
			"admin_id = (?)",
			tx.Model(&database.AdminModel{}).Select("id").Where("username = ?", username),
		).Delete(&database.SessionModel{}).Error; err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
		return nil
	})
}

func (s *Store) ChangeAdminPassword(
	ctx context.Context,
	username, passwordHash string,
	adminID, keepSessionID int64,
) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&database.AdminModel{}).
			Where("username = ? AND id = ?", username, adminID).
			Updates(map[string]any{
				"password_hash": passwordHash,
				"updated_at":    s.now(),
			})
		if result.Error != nil {
			return fmt.Errorf("change admin password: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		if err := tx.Where(
			"admin_id = ? AND id <> ?", adminID, keepSessionID,
		).Delete(&database.SessionModel{}).Error; err != nil {
			return fmt.Errorf("revoke other sessions: %w", err)
		}
		return nil
	})
}

func (s *Store) CreateSession(
	ctx context.Context,
	adminID int64,
	tokenHash []byte,
	csrfToken string,
	expiresAt time.Time,
) (int64, error) {
	now := s.now()
	model := database.SessionModel{
		AdminID:    adminID,
		TokenHash:  tokenHash,
		CSRFToken:  csrfToken,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  expiresAt.UTC(),
	}
	if err := s.db.WithContext(ctx).Omit(clause.Associations).Create(&model).Error; err != nil {
		return 0, fmt.Errorf("create session: %w", err)
	}
	return model.ID, nil
}

func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash []byte) (Session, error) {
	var row struct {
		ID         int64
		AdminID    int64
		Username   string
		CSRFToken  string
		ExpiresAt  time.Time
		LastSeenAt time.Time
	}
	err := s.db.WithContext(ctx).
		Table("sessions AS s").
		Select("s.id, s.admin_id, a.username, s.csrf_token, s.expires_at, s.last_seen_at").
		Joins("JOIN admins a ON a.id = s.admin_id").
		Where("s.token_hash = ?", tokenHash).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("find session: %w", err)
	}
	if !row.ExpiresAt.After(s.now()) {
		_ = s.DeleteSession(ctx, tokenHash)
		return Session{}, ErrNotFound
	}
	// Refresh last_seen_at at most every five minutes; deciding in Go keeps
	// ordinary authenticated reads from issuing a write statement.
	now := s.now()
	if row.LastSeenAt.Before(now.Add(-5 * time.Minute)) {
		_ = s.db.WithContext(ctx).
			Model(&database.SessionModel{}).
			Where("id = ?", row.ID).
			Update("last_seen_at", now).Error
	}
	return Session{
		ID:        row.ID,
		AdminID:   row.AdminID,
		Username:  row.Username,
		CSRFToken: row.CSRFToken,
		ExpiresAt: row.ExpiresAt,
	}, nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	if err := s.db.WithContext(ctx).
		Where("token_hash = ?", tokenHash).
		Delete(&database.SessionModel{}).Error; err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Store) DeleteOtherSessions(ctx context.Context, adminID, keepSessionID int64) error {
	if err := s.db.WithContext(ctx).
		Where("admin_id = ? AND id <> ?", adminID, keepSessionID).
		Delete(&database.SessionModel{}).Error; err != nil {
		return fmt.Errorf("delete other sessions: %w", err)
	}
	return nil
}

func (s *Store) CleanupSessions(ctx context.Context) error {
	if err := s.db.WithContext(ctx).
		Where("expires_at <= ?", s.now()).
		Delete(&database.SessionModel{}).Error; err != nil {
		return fmt.Errorf("cleanup sessions: %w", err)
	}
	return nil
}

func (s *Store) Settings(ctx context.Context) (domain.Settings, error) {
	var model database.SettingsModel
	if err := s.db.WithContext(ctx).First(&model, 1).Error; err != nil {
		return domain.Settings{}, fmt.Errorf("read settings: %w", err)
	}
	return settingsFromModel(model), nil
}

func (s *Store) UpdateSettings(ctx context.Context, settings domain.Settings) (domain.Settings, error) {
	if settings.PollIntervalMinutes < 1 || settings.PollIntervalMinutes > 1440 {
		return domain.Settings{}, errors.New("poll interval must be between 1 and 1440 minutes")
	}
	if settings.SeedTimeMinutes != nil &&
		(*settings.SeedTimeMinutes < 0 ||
			math.IsNaN(*settings.SeedTimeMinutes) ||
			math.IsInf(*settings.SeedTimeMinutes, 0)) {
		return domain.Settings{}, errors.New("seed time must be a finite non-negative number")
	}
	if settings.SeedRatio != nil &&
		(*settings.SeedRatio < 0 ||
			math.IsNaN(*settings.SeedRatio) ||
			math.IsInf(*settings.SeedRatio, 0)) {
		return domain.Settings{}, errors.New("seed ratio must be a finite non-negative number")
	}
	err := s.db.WithContext(ctx).
		Model(&database.SettingsModel{}).
		Where("id = 1").
		Updates(map[string]any{
			"aria2_url":             settings.Aria2URL,
			"aria2_secret":          settings.Aria2Secret,
			"seed_time_minutes":     settings.SeedTimeMinutes,
			"seed_ratio":            settings.SeedRatio,
			"proxy_url":             settings.ProxyURL,
			"proxy_username":        settings.ProxyUsername,
			"proxy_password":        settings.ProxyPassword,
			"default_download_dir":  settings.DefaultDownloadDir,
			"poll_interval_minutes": settings.PollIntervalMinutes,
			"updated_at":            s.now(),
		}).Error
	if err != nil {
		return domain.Settings{}, fmt.Errorf("update settings: %w", err)
	}
	return s.Settings(ctx)
}

func (s *Store) CreateSubscription(
	ctx context.Context,
	name, rawURL, downloadDir string,
	downloadExisting bool,
) (domain.Subscription, error) {
	now := s.now()
	model := database.SubscriptionModel{
		Name:                    strings.TrimSpace(name),
		URL:                     rawURL,
		Enabled:                 true,
		DownloadDir:             strings.TrimSpace(downloadDir),
		ETag:                    "",
		LastModified:            "",
		InitialSyncDone:         false,
		InitialDownloadExisting: downloadExisting,
		NextCheckAt:             &now,
		LastError:               "",
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	if err := s.db.WithContext(ctx).Omit(clause.Associations).Create(&model).Error; err != nil {
		return domain.Subscription{}, fmt.Errorf("create subscription: %w", err)
	}
	return subscriptionFromModel(model), nil
}

type SubscriptionPatch struct {
	Name        *string
	URL         *string
	Enabled     *bool
	DownloadDir *string
}

func (s *Store) UpdateSubscription(
	ctx context.Context,
	id int64,
	patch SubscriptionPatch,
) (domain.Subscription, error) {
	updates := map[string]any{"updated_at": s.now()}
	if patch.Name != nil {
		updates["name"] = strings.TrimSpace(*patch.Name)
	}
	if patch.URL != nil {
		updates["url"] = *patch.URL
		updates["etag"] = ""
		updates["last_modified"] = ""
		updates["initial_sync_done"] = false
		updates["initial_download_existing"] = false
		now := s.now()
		updates["next_check_at"] = &now
	}
	if patch.Enabled != nil {
		updates["enabled"] = *patch.Enabled
		if *patch.Enabled {
			now := s.now()
			updates["next_check_at"] = &now
		}
	}
	if patch.DownloadDir != nil {
		updates["download_dir"] = strings.TrimSpace(*patch.DownloadDir)
	}
	result := s.db.WithContext(ctx).
		Model(&database.SubscriptionModel{}).
		Where("id = ?", id).
		Updates(updates)
	if result.Error != nil {
		return domain.Subscription{}, fmt.Errorf("update subscription: %w", result.Error)
	}
	// updates always carries updated_at, so an existing row is always affected.
	if result.RowsAffected == 0 {
		return domain.Subscription{}, ErrNotFound
	}
	return s.Subscription(ctx, id)
}

func (s *Store) ArchiveSubscription(ctx context.Context, id int64) error {
	now := s.now()
	result := s.db.WithContext(ctx).
		Model(&database.SubscriptionModel{}).
		Where("id = ? AND archived_at IS NULL", id).
		Updates(map[string]any{
			"enabled":     false,
			"archived_at": &now,
			"updated_at":  now,
		})
	if result.Error != nil {
		return fmt.Errorf("archive subscription: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		if _, err := s.Subscription(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RestoreSubscription(ctx context.Context, id int64) error {
	now := s.now()
	result := s.db.WithContext(ctx).
		Model(&database.SubscriptionModel{}).
		Where("id = ? AND archived_at IS NOT NULL", id).
		Updates(map[string]any{
			"archived_at":   nil,
			"enabled":       true,
			"next_check_at": &now,
			"updated_at":    now,
		})
	if result.Error != nil {
		return fmt.Errorf("restore subscription: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		if _, err := s.Subscription(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Subscription(ctx context.Context, id int64) (domain.Subscription, error) {
	var model database.SubscriptionModel
	err := s.db.WithContext(ctx).First(&model, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.Subscription{}, ErrNotFound
	}
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("read subscription: %w", err)
	}
	return subscriptionFromModel(model), nil
}

func (s *Store) ListSubscriptions(ctx context.Context, includeArchived bool) ([]domain.Subscription, error) {
	query := s.db.WithContext(ctx).Model(&database.SubscriptionModel{})
	if !includeArchived {
		query = query.Where("archived_at IS NULL")
	}
	var models []database.SubscriptionModel
	if err := query.Order("archived_at IS NOT NULL ASC, created_at DESC").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	result := make([]domain.Subscription, 0, len(models))
	for _, model := range models {
		result = append(result, subscriptionFromModel(model))
	}
	return result, nil
}

func (s *Store) DueSubscriptions(ctx context.Context, limit int) ([]domain.Subscription, error) {
	var models []database.SubscriptionModel
	err := s.db.WithContext(ctx).
		Where("enabled = ? AND archived_at IS NULL", true).
		Where("next_check_at IS NULL OR next_check_at <= ?", s.now()).
		Order("next_check_at ASC").
		Limit(limit).
		Find(&models).Error
	if err != nil {
		return nil, fmt.Errorf("list due subscriptions: %w", err)
	}
	result := make([]domain.Subscription, 0, len(models))
	for _, model := range models {
		result = append(result, subscriptionFromModel(model))
	}
	return result, nil
}

func (s *Store) ApplyFeedSync(
	ctx context.Context,
	subscriptionID int64,
	channelTitle, etag, lastModified string,
	items []domain.ParsedItem,
	nextCheck time.Time,
) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var subscription database.SubscriptionModel
		if err := tx.First(&subscription, subscriptionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("read subscription sync state: %w", err)
		}
		autoQueue := subscription.InitialSyncDone || subscription.InitialDownloadExisting
		now := s.now()
		for _, item := range items {
			model := feedItemModel(subscriptionID, item, now)
			result := tx.Omit(clause.Associations).
				Clauses(clause.OnConflict{DoNothing: true}).
				Create(&model)
			if result.Error != nil {
				return fmt.Errorf("insert feed item: %w", result.Error)
			}
			if result.RowsAffected == 0 ||
				!autoQueue ||
				item.RecognitionStatus != domain.RecognitionRecognized {
				continue
			}
			if _, err := ensureDownloadTx(tx, model.ID, item.ReleaseKey, item.EnclosureURL, now); err != nil {
				return err
			}
		}
		name := subscription.Name
		if name == "" {
			name = strings.TrimSpace(channelTitle)
		}
		err := tx.Model(&database.SubscriptionModel{}).
			Where("id = ?", subscriptionID).
			Updates(map[string]any{
				"name":              name,
				"etag":              etag,
				"last_modified":     lastModified,
				"initial_sync_done": true,
				"last_checked_at":   &now,
				"next_check_at":     nextCheck.UTC(),
				"last_error":        "",
				"updated_at":        now,
			}).Error
		if err != nil {
			return fmt.Errorf("finish feed sync: %w", err)
		}
		return nil
	})
}

func (s *Store) MarkSyncNotModified(
	ctx context.Context,
	id int64,
	etag, lastModified string,
	nextCheck time.Time,
) error {
	now := s.now()
	result := s.db.WithContext(ctx).
		Model(&database.SubscriptionModel{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"etag":              etag,
			"last_modified":     lastModified,
			"initial_sync_done": true,
			"last_checked_at":   &now,
			"next_check_at":     nextCheck.UTC(),
			"last_error":        "",
			"updated_at":        now,
		})
	if result.Error != nil {
		return fmt.Errorf("mark feed unchanged: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MarkSyncError(ctx context.Context, id int64, message string, nextCheck time.Time) error {
	now := s.now()
	result := s.db.WithContext(ctx).
		Model(&database.SubscriptionModel{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_checked_at": &now,
			"next_check_at":   nextCheck.UTC(),
			"last_error":      truncate(message, 2000),
			"updated_at":      now,
		})
	if result.Error != nil {
		return fmt.Errorf("record feed error: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) QueueItemDownload(ctx context.Context, itemID int64, force bool) (domain.Download, error) {
	var downloadID int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item database.FeedItemModel
		if err := tx.First(&item, itemID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("read item for download: %w", err)
		}
		if item.RecognitionStatus != string(domain.RecognitionRecognized) || item.EnclosureURL == "" {
			return errors.New("item is not a recognized torrent")
		}
		if item.DownloadID != nil {
			downloadID = *item.DownloadID
			if force {
				now := s.now()
				if err := tx.Model(&database.DownloadModel{}).
					Where("id = ?", downloadID).
					Updates(map[string]any{
						"status":        domain.DownloadQueued,
						"error_message": "",
						"retry_count":   0,
						"next_retry_at": &now,
						"updated_at":    now,
					}).Error; err != nil {
					return fmt.Errorf("reset download: %w", err)
				}
			}
			return nil
		}
		sum := sha256.Sum256([]byte(item.EnclosureURL))
		releaseKey := hex.EncodeToString(sum[:])
		id, err := ensureDownloadTx(tx, item.ID, releaseKey, item.EnclosureURL, s.now())
		if err != nil {
			return err
		}
		downloadID = id
		return nil
	})
	if err != nil {
		return domain.Download{}, err
	}
	return s.Download(ctx, downloadID)
}

func (s *Store) Download(ctx context.Context, id int64) (domain.Download, error) {
	var model database.DownloadModel
	err := s.db.WithContext(ctx).First(&model, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.Download{}, ErrNotFound
	}
	if err != nil {
		return domain.Download{}, fmt.Errorf("read download: %w", err)
	}
	return downloadFromModel(model), nil
}

func (s *Store) DownloadDirectory(ctx context.Context, id int64) (string, error) {
	var row struct{ DownloadDir string }
	err := s.db.WithContext(ctx).
		Table("feed_items AS fi").
		Select("s.download_dir").
		Joins("JOIN subscriptions s ON s.id = fi.subscription_id").
		Where("fi.download_id = ?", id).
		Order("fi.id ASC").
		Limit(1).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read download directory: %w", err)
	}
	return row.DownloadDir, nil
}

func (s *Store) ResetDownload(ctx context.Context, id int64) (domain.Download, error) {
	now := s.now()
	result := s.db.WithContext(ctx).
		Model(&database.DownloadModel{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":        domain.DownloadQueued,
			"error_message": "",
			"retry_count":   0,
			"next_retry_at": &now,
			"updated_at":    now,
		})
	if result.Error != nil {
		return domain.Download{}, fmt.Errorf("reset download: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.Download{}, ErrNotFound
	}
	return s.Download(ctx, id)
}

func (s *Store) PendingDownloads(ctx context.Context, limit int) ([]domain.Download, error) {
	var models []database.DownloadModel
	err := s.db.WithContext(ctx).
		Where("status = ?", domain.DownloadQueued).
		Where("next_retry_at IS NULL OR next_retry_at <= ?", s.now()).
		Order("next_retry_at ASC, id ASC").
		Limit(limit).
		Find(&models).Error
	if err != nil {
		return nil, fmt.Errorf("list pending downloads: %w", err)
	}
	return downloadsFromModels(models), nil
}

func (s *Store) TrackableDownloads(ctx context.Context, limit int) ([]domain.Download, error) {
	var models []database.DownloadModel
	err := s.db.WithContext(ctx).
		Where("aria2_gid <> ''").
		Where("status IN ?", []domain.DownloadStatus{
			domain.DownloadSubmitting,
			domain.DownloadWaiting,
			domain.DownloadActive,
			domain.DownloadPaused,
		}).
		Order("COALESCE(last_checked_at, created_at) ASC, id ASC").
		Limit(limit).
		Find(&models).Error
	if err != nil {
		return nil, fmt.Errorf("list trackable downloads: %w", err)
	}
	return downloadsFromModels(models), nil
}

func (s *Store) MarkDownloadSubmitting(ctx context.Context, id int64) error {
	err := s.db.WithContext(ctx).
		Model(&database.DownloadModel{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":        domain.DownloadSubmitting,
			"error_message": "",
			"updated_at":    s.now(),
		}).Error
	if err != nil {
		return fmt.Errorf("mark download submitting: %w", err)
	}
	return nil
}

func (s *Store) UpdateDownloadStatus(
	ctx context.Context,
	id int64,
	status domain.DownloadStatus,
	totalLength, completedLength int64,
	message string,
) error {
	now := s.now()
	err := s.db.WithContext(ctx).
		Model(&database.DownloadModel{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":           status,
			"total_length":     totalLength,
			"completed_length": completedLength,
			"error_message":    truncate(message, 2000),
			"next_retry_at":    nil,
			"last_checked_at":  &now,
			"updated_at":       now,
		}).Error
	if err != nil {
		return fmt.Errorf("update download status: %w", err)
	}
	return nil
}

func (s *Store) MarkDownloadRetry(
	ctx context.Context,
	id int64,
	message string,
	nextRetry time.Time,
	permanent bool,
) error {
	status := domain.DownloadQueued
	var next any = nextRetry.UTC()
	if permanent {
		status = domain.DownloadError
		next = nil
	}
	err := s.db.WithContext(ctx).
		Model(&database.DownloadModel{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":        status,
			"error_message": truncate(message, 2000),
			"retry_count":   gorm.Expr("retry_count + 1"),
			"next_retry_at": next,
			"updated_at":    s.now(),
		}).Error
	if err != nil {
		return fmt.Errorf("record download retry: %w", err)
	}
	return nil
}

func (s *Store) MarkDownloadCheckError(ctx context.Context, id int64, message string) error {
	now := s.now()
	err := s.db.WithContext(ctx).
		Model(&database.DownloadModel{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"error_message":   truncate(message, 2000),
			"last_checked_at": &now,
			"updated_at":      now,
		}).Error
	if err != nil {
		return fmt.Errorf("record status check error: %w", err)
	}
	return nil
}

type ItemFilter struct {
	Page           int
	PageSize       int
	SubscriptionID int64
	Recognition    string
	DownloadStatus string
	Search         string
}

func (s *Store) ListItems(ctx context.Context, filter ItemFilter) (domain.Page[domain.FeedItem], error) {
	filter.Page, filter.PageSize = normalizePage(filter.Page, filter.PageSize)
	var total int64
	if err := s.itemQuery(ctx, filter).Count(&total).Error; err != nil {
		return domain.Page[domain.FeedItem]{}, fmt.Errorf("count items: %w", err)
	}
	var models []database.FeedItemModel
	err := s.itemQuery(ctx, filter).
		Preload("Subscription").
		Preload("Download").
		Order("COALESCE(feed_items.published_at, feed_items.discovered_at) DESC").
		Order("feed_items.id DESC").
		Limit(filter.PageSize).
		Offset((filter.Page - 1) * filter.PageSize).
		Find(&models).Error
	if err != nil {
		return domain.Page[domain.FeedItem]{}, fmt.Errorf("list items: %w", err)
	}
	items := make([]domain.FeedItem, 0, len(models))
	for _, model := range models {
		items = append(items, itemFromModel(model))
	}
	return domain.Page[domain.FeedItem]{
		Items:      items,
		Page:       filter.Page,
		PageSize:   filter.PageSize,
		Total:      total,
		TotalPages: int(math.Ceil(float64(total) / float64(filter.PageSize))),
	}, nil
}

func (s *Store) Item(ctx context.Context, id int64) (domain.FeedItem, error) {
	var model database.FeedItemModel
	err := s.db.WithContext(ctx).
		Preload("Subscription").
		Preload("Download").
		First(&model, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.FeedItem{}, ErrNotFound
	}
	if err != nil {
		return domain.FeedItem{}, fmt.Errorf("read item: %w", err)
	}
	return itemFromModel(model), nil
}

func (s *Store) Dashboard(ctx context.Context) (domain.Dashboard, error) {
	db := s.db.WithContext(ctx)
	var result domain.Dashboard
	if err := db.Model(&database.SubscriptionModel{}).
		Where("archived_at IS NULL").
		Count(&result.Subscriptions).Error; err != nil {
		return domain.Dashboard{}, fmt.Errorf("count subscriptions: %w", err)
	}
	if err := db.Model(&database.FeedItemModel{}).
		Where("recognition_status = ?", domain.RecognitionRecognized).
		Count(&result.RecognizedItems).Error; err != nil {
		return domain.Dashboard{}, fmt.Errorf("count recognized items: %w", err)
	}
	var downloadCounts []struct {
		Status string
		Count  int64
	}
	if err := db.Model(&database.DownloadModel{}).
		Select("status, COUNT(*) AS count").
		Group("status").
		Scan(&downloadCounts).Error; err != nil {
		return domain.Dashboard{}, fmt.Errorf("count downloads: %w", err)
	}
	for _, entry := range downloadCounts {
		switch domain.DownloadStatus(entry.Status) {
		case domain.DownloadQueued, domain.DownloadSubmitting, domain.DownloadWaiting,
			domain.DownloadActive, domain.DownloadPaused:
			result.ActiveDownloads += entry.Count
		case domain.DownloadComplete:
			result.Completed += entry.Count
		case domain.DownloadError, domain.DownloadUnknown:
			result.Errors += entry.Count
		}
	}
	var last struct {
		LastSyncAt *time.Time
	}
	if err := db.Model(&database.SubscriptionModel{}).
		Select("MAX(last_checked_at) AS last_sync_at").
		Scan(&last).Error; err != nil {
		return domain.Dashboard{}, fmt.Errorf("read last synchronization: %w", err)
	}
	result.LastSyncAt = last.LastSyncAt
	return result, nil
}

func ensureDownloadTx(
	tx *gorm.DB,
	itemID int64,
	releaseKey, enclosureURL string,
	now time.Time,
) (int64, error) {
	model := database.DownloadModel{
		ReleaseKey:      releaseKey,
		EnclosureURL:    enclosureURL,
		Aria2GID:        stableGID(releaseKey),
		Status:          string(domain.DownloadQueued),
		TotalLength:     0,
		CompletedLength: 0,
		ErrorMessage:    "",
		RetryCount:      0,
		NextRetryAt:     &now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := tx.Omit(clause.Associations).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "release_key"}},
			DoNothing: true,
		}).
		Create(&model).Error; err != nil {
		return 0, fmt.Errorf("create download: %w", err)
	}
	if err := tx.Where("release_key = ?", releaseKey).First(&model).Error; err != nil {
		return 0, fmt.Errorf("find download: %w", err)
	}
	if err := tx.Model(&database.FeedItemModel{}).
		Where("id = ?", itemID).
		Update("download_id", model.ID).Error; err != nil {
		return 0, fmt.Errorf("link item download: %w", err)
	}
	return model.ID, nil
}

func (s *Store) itemQuery(ctx context.Context, filter ItemFilter) *gorm.DB {
	query := s.db.WithContext(ctx).
		Model(&database.FeedItemModel{})
	if filter.SubscriptionID > 0 {
		query = query.Where("feed_items.subscription_id = ?", filter.SubscriptionID)
	}
	if filter.Recognition != "" {
		query = query.Where("feed_items.recognition_status = ?", filter.Recognition)
	}
	if filter.DownloadStatus != "" {
		if filter.DownloadStatus == string(domain.DownloadNotRequested) {
			query = query.Where("feed_items.download_id IS NULL")
		} else {
			query = query.
				Joins("LEFT JOIN downloads d ON d.id = feed_items.download_id").
				Where("d.status = ?", filter.DownloadStatus)
		}
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		pattern := "%" + escapeLike(search) + "%"
		query = query.Where(
			"(feed_items.title LIKE ? ESCAPE '\\' OR feed_items.description LIKE ? ESCAPE '\\')",
			pattern,
			pattern,
		)
	}
	return query
}

func settingsFromModel(model database.SettingsModel) domain.Settings {
	return domain.Settings{
		Aria2URL:            model.Aria2URL,
		Aria2Secret:         model.Aria2Secret,
		SeedTimeMinutes:     model.SeedTimeMinutes,
		SeedRatio:           model.SeedRatio,
		ProxyURL:            model.ProxyURL,
		ProxyUsername:       model.ProxyUsername,
		ProxyPassword:       model.ProxyPassword,
		DefaultDownloadDir:  model.DefaultDownloadDir,
		PollIntervalMinutes: model.PollIntervalMinutes,
		UpdatedAt:           model.UpdatedAt.UTC(),
	}
}

func subscriptionFromModel(model database.SubscriptionModel) domain.Subscription {
	return domain.Subscription{
		ID:                      model.ID,
		Name:                    model.Name,
		URL:                     model.URL,
		Enabled:                 model.Enabled,
		DownloadDir:             model.DownloadDir,
		ETag:                    model.ETag,
		LastModified:            model.LastModified,
		InitialSyncDone:         model.InitialSyncDone,
		InitialDownloadExisting: model.InitialDownloadExisting,
		LastCheckedAt:           utcPointer(model.LastCheckedAt),
		NextCheckAt:             utcPointer(model.NextCheckAt),
		LastError:               model.LastError,
		ArchivedAt:              utcPointer(model.ArchivedAt),
		CreatedAt:               model.CreatedAt.UTC(),
		UpdatedAt:               model.UpdatedAt.UTC(),
	}
}

func downloadFromModel(model database.DownloadModel) domain.Download {
	return domain.Download{
		ID:              model.ID,
		ReleaseKey:      model.ReleaseKey,
		EnclosureURL:    model.EnclosureURL,
		Aria2GID:        model.Aria2GID,
		Status:          domain.DownloadStatus(model.Status),
		TotalLength:     model.TotalLength,
		CompletedLength: model.CompletedLength,
		ErrorMessage:    model.ErrorMessage,
		RetryCount:      model.RetryCount,
		NextRetryAt:     utcPointer(model.NextRetryAt),
		LastCheckedAt:   utcPointer(model.LastCheckedAt),
		CreatedAt:       model.CreatedAt.UTC(),
		UpdatedAt:       model.UpdatedAt.UTC(),
	}
}

func downloadsFromModels(models []database.DownloadModel) []domain.Download {
	result := make([]domain.Download, 0, len(models))
	for _, model := range models {
		result = append(result, downloadFromModel(model))
	}
	return result
}

func feedItemModel(subscriptionID int64, item domain.ParsedItem, discoveredAt time.Time) database.FeedItemModel {
	return database.FeedItemModel{
		SubscriptionID:    subscriptionID,
		ItemKey:           item.ItemKey,
		GUID:              item.GUID,
		Title:             item.Title,
		Description:       item.Description,
		DetailURL:         item.DetailURL,
		EnclosureURL:      item.EnclosureURL,
		EnclosureLength:   item.EnclosureLength,
		PublishedAt:       utcPointer(item.PublishedAt),
		DiscoveredAt:      discoveredAt.UTC(),
		RecognitionStatus: string(item.RecognitionStatus),
		RecognitionError:  item.RecognitionError,
	}
}

func itemFromModel(model database.FeedItemModel) domain.FeedItem {
	item := domain.FeedItem{
		ID:                model.ID,
		SubscriptionID:    model.SubscriptionID,
		SubscriptionName:  model.Subscription.Name,
		ItemKey:           model.ItemKey,
		GUID:              model.GUID,
		Title:             model.Title,
		Description:       model.Description,
		DetailURL:         model.DetailURL,
		EnclosureURL:      model.EnclosureURL,
		EnclosureLength:   model.EnclosureLength,
		PublishedAt:       utcPointer(model.PublishedAt),
		DiscoveredAt:      model.DiscoveredAt.UTC(),
		RecognitionStatus: domain.RecognitionStatus(model.RecognitionStatus),
		RecognitionError:  model.RecognitionError,
		DownloadID:        model.DownloadID,
	}
	if model.Download != nil {
		download := downloadFromModel(*model.Download)
		item.Download = &download
	}
	return item
}

func stableGID(releaseKey string) string {
	if len(releaseKey) >= 16 {
		gid := strings.ToLower(releaseKey[:16])
		if gid != "0000000000000000" {
			return gid
		}
	}
	sum := sha256.Sum256([]byte(releaseKey))
	gid := hex.EncodeToString(sum[:8])
	if gid == "0000000000000000" {
		return "0000000000000001"
	}
	return gid
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func utcPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
