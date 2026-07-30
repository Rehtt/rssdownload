package database

import "time"

type AdminModel struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement:false"`
	Username     string    `gorm:"column:username;not null;uniqueIndex"`
	PasswordHash string    `gorm:"column:password_hash;not null"`
	CreatedAt    time.Time `gorm:"column:created_at;not null"`
	UpdatedAt    time.Time `gorm:"column:updated_at;not null"`
}

func (AdminModel) TableName() string { return "admins" }

type SessionModel struct {
	ID         int64      `gorm:"column:id;primaryKey;autoIncrement"`
	AdminID    int64      `gorm:"column:admin_id;not null;index"`
	Admin      AdminModel `gorm:"foreignKey:AdminID;constraint:OnDelete:CASCADE"`
	TokenHash  []byte     `gorm:"column:token_hash;not null;uniqueIndex"`
	CSRFToken  string     `gorm:"column:csrf_token;not null"`
	CreatedAt  time.Time  `gorm:"column:created_at;not null"`
	LastSeenAt time.Time  `gorm:"column:last_seen_at;not null"`
	ExpiresAt  time.Time  `gorm:"column:expires_at;not null;index:idx_sessions_expires_at"`
}

func (SessionModel) TableName() string { return "sessions" }

type SettingsModel struct {
	ID                  int64     `gorm:"column:id;primaryKey;autoIncrement:false"`
	Aria2URL            string    `gorm:"column:aria2_url;not null"`
	Aria2Secret         string    `gorm:"column:aria2_secret;not null"`
	SeedTimeMinutes     *float64  `gorm:"column:seed_time_minutes"`
	SeedRatio           *float64  `gorm:"column:seed_ratio"`
	ProxyURL            string    `gorm:"column:proxy_url;not null;default:''"`
	ProxyUsername       string    `gorm:"column:proxy_username;not null;default:''"`
	ProxyPassword       string    `gorm:"column:proxy_password;not null;default:''"`
	DefaultDownloadDir  string    `gorm:"column:default_download_dir;not null"`
	PollIntervalMinutes int       `gorm:"column:poll_interval_minutes;not null;check:poll_interval_minutes BETWEEN 1 AND 1440"`
	CreatedAt           time.Time `gorm:"column:created_at;not null"`
	UpdatedAt           time.Time `gorm:"column:updated_at;not null"`
}

func (SettingsModel) TableName() string { return "settings" }

type SubscriptionModel struct {
	ID                      int64      `gorm:"column:id;primaryKey;autoIncrement"`
	Name                    string     `gorm:"column:name;not null"`
	URL                     string     `gorm:"column:url;not null"`
	Enabled                 bool       `gorm:"column:enabled;not null;default:true;index:idx_subscriptions_due,priority:1"`
	DownloadDir             string     `gorm:"column:download_dir;not null"`
	ETag                    string     `gorm:"column:etag;not null"`
	LastModified            string     `gorm:"column:last_modified;not null"`
	InitialSyncDone         bool       `gorm:"column:initial_sync_done;not null"`
	InitialDownloadExisting bool       `gorm:"column:initial_download_existing;not null"`
	LastCheckedAt           *time.Time `gorm:"column:last_checked_at"`
	NextCheckAt             *time.Time `gorm:"column:next_check_at;index:idx_subscriptions_due,priority:3"`
	LastError               string     `gorm:"column:last_error;not null"`
	ArchivedAt              *time.Time `gorm:"column:archived_at;index:idx_subscriptions_due,priority:2"`
	CreatedAt               time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt               time.Time  `gorm:"column:updated_at;not null"`
}

func (SubscriptionModel) TableName() string { return "subscriptions" }

type DownloadModel struct {
	ID              int64      `gorm:"column:id;primaryKey;autoIncrement"`
	ReleaseKey      string     `gorm:"column:release_key;not null;uniqueIndex"`
	EnclosureURL    string     `gorm:"column:enclosure_url;not null"`
	Aria2GID        string     `gorm:"column:aria2_gid;not null;uniqueIndex"`
	Status          string     `gorm:"column:status;not null;index:idx_downloads_pending,priority:1"`
	TotalLength     int64      `gorm:"column:total_length;not null"`
	CompletedLength int64      `gorm:"column:completed_length;not null"`
	ErrorMessage    string     `gorm:"column:error_message;not null"`
	RetryCount      int        `gorm:"column:retry_count;not null"`
	NextRetryAt     *time.Time `gorm:"column:next_retry_at;index:idx_downloads_pending,priority:2"`
	LastCheckedAt   *time.Time `gorm:"column:last_checked_at"`
	CreatedAt       time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;not null"`
}

func (DownloadModel) TableName() string { return "downloads" }

type FeedItemModel struct {
	ID                int64             `gorm:"column:id;primaryKey;autoIncrement"`
	SubscriptionID    int64             `gorm:"column:subscription_id;not null;uniqueIndex:idx_feed_item_subscription_key,priority:1"`
	Subscription      SubscriptionModel `gorm:"foreignKey:SubscriptionID;constraint:OnDelete:RESTRICT"`
	ItemKey           string            `gorm:"column:item_key;not null;uniqueIndex:idx_feed_item_subscription_key,priority:2"`
	GUID              string            `gorm:"column:guid;not null"`
	Title             string            `gorm:"column:title;not null"`
	Description       string            `gorm:"column:description;not null"`
	DetailURL         string            `gorm:"column:detail_url;not null"`
	EnclosureURL      string            `gorm:"column:enclosure_url;not null"`
	EnclosureLength   int64             `gorm:"column:enclosure_length;not null"`
	PublishedAt       *time.Time        `gorm:"column:published_at;index:idx_feed_items_published,priority:1,sort:desc"`
	DiscoveredAt      time.Time         `gorm:"column:discovered_at;not null;index:idx_feed_items_published,priority:2,sort:desc"`
	RecognitionStatus string            `gorm:"column:recognition_status;not null"`
	RecognitionError  string            `gorm:"column:recognition_error;not null"`
	DownloadID        *int64            `gorm:"column:download_id;index:idx_feed_items_download"`
	Download          *DownloadModel    `gorm:"foreignKey:DownloadID;constraint:OnDelete:SET NULL"`
}

func (FeedItemModel) TableName() string { return "feed_items" }
