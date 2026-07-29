package domain

import "time"

type RecognitionStatus string

const (
	RecognitionRecognized  RecognitionStatus = "recognized"
	RecognitionUnsupported RecognitionStatus = "unsupported"
)

type DownloadStatus string

const (
	DownloadNotRequested DownloadStatus = "not_requested"
	DownloadQueued       DownloadStatus = "queued"
	DownloadSubmitting   DownloadStatus = "submitting"
	DownloadWaiting      DownloadStatus = "waiting"
	DownloadActive       DownloadStatus = "active"
	DownloadPaused       DownloadStatus = "paused"
	DownloadComplete     DownloadStatus = "complete"
	DownloadError        DownloadStatus = "error"
	DownloadUnknown      DownloadStatus = "unknown"
)

type Settings struct {
	Aria2URL            string    `json:"aria2Url"`
	Aria2Secret         string    `json:"aria2Secret,omitempty"`
	ProxyURL            string    `json:"proxyUrl"`
	ProxyUsername       string    `json:"proxyUsername"`
	ProxyPassword       string    `json:"proxyPassword,omitempty"`
	DefaultDownloadDir  string    `json:"defaultDownloadDir"`
	PollIntervalMinutes int       `json:"pollIntervalMinutes"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type Subscription struct {
	ID                      int64      `json:"id"`
	Name                    string     `json:"name"`
	URL                     string     `json:"url"`
	Enabled                 bool       `json:"enabled"`
	DownloadDir             string     `json:"downloadDir"`
	ETag                    string     `json:"-"`
	LastModified            string     `json:"-"`
	InitialSyncDone         bool       `json:"initialSyncDone"`
	InitialDownloadExisting bool       `json:"initialDownloadExisting"`
	LastCheckedAt           *time.Time `json:"lastCheckedAt"`
	NextCheckAt             *time.Time `json:"nextCheckAt"`
	LastError               string     `json:"lastError"`
	ArchivedAt              *time.Time `json:"archivedAt"`
	CreatedAt               time.Time  `json:"createdAt"`
	UpdatedAt               time.Time  `json:"updatedAt"`
}

type FeedItem struct {
	ID                int64             `json:"id"`
	SubscriptionID    int64             `json:"subscriptionId"`
	SubscriptionName  string            `json:"subscriptionName,omitempty"`
	ItemKey           string            `json:"-"`
	GUID              string            `json:"guid"`
	Title             string            `json:"title"`
	Description       string            `json:"description"`
	DetailURL         string            `json:"detailUrl"`
	EnclosureURL      string            `json:"enclosureUrl"`
	EnclosureLength   int64             `json:"enclosureLength"`
	PublishedAt       *time.Time        `json:"publishedAt"`
	DiscoveredAt      time.Time         `json:"discoveredAt"`
	RecognitionStatus RecognitionStatus `json:"recognitionStatus"`
	RecognitionError  string            `json:"recognitionError"`
	DownloadID        *int64            `json:"downloadId"`
	Download          *Download         `json:"download,omitempty"`
}

type ParsedItem struct {
	ItemKey           string
	ReleaseKey        string
	GUID              string
	Title             string
	Description       string
	DetailURL         string
	EnclosureURL      string
	EnclosureLength   int64
	PublishedAt       *time.Time
	RecognitionStatus RecognitionStatus
	RecognitionError  string
}

type Download struct {
	ID              int64          `json:"id"`
	ReleaseKey      string         `json:"-"`
	EnclosureURL    string         `json:"-"`
	Aria2GID        string         `json:"aria2Gid"`
	Status          DownloadStatus `json:"status"`
	TotalLength     int64          `json:"totalLength"`
	CompletedLength int64          `json:"completedLength"`
	ErrorMessage    string         `json:"errorMessage"`
	RetryCount      int            `json:"retryCount"`
	NextRetryAt     *time.Time     `json:"nextRetryAt"`
	LastCheckedAt   *time.Time     `json:"lastCheckedAt"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}

type Dashboard struct {
	Subscriptions   int64      `json:"subscriptions"`
	RecognizedItems int64      `json:"recognizedItems"`
	ActiveDownloads int64      `json:"activeDownloads"`
	Completed       int64      `json:"completed"`
	Errors          int64      `json:"errors"`
	LastSyncAt      *time.Time `json:"lastSyncAt"`
}

type Page[T any] struct {
	Items      []T   `json:"items"`
	Page       int   `json:"page"`
	PageSize   int   `json:"pageSize"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"totalPages"`
}
