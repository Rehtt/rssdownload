package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Rehtt/rssdownload/internal/aria2"
	"github.com/Rehtt/rssdownload/internal/domain"
	"github.com/Rehtt/rssdownload/internal/feed"
	proxyconfig "github.com/Rehtt/rssdownload/internal/proxy"
	"github.com/Rehtt/rssdownload/internal/store"
)

const maxTorrentSize = 10 << 20

var ErrBusy = errors.New("operation already in progress")

type FeedFetcher interface {
	Fetch(ctx context.Context, rawURL, etag, lastModified string) (feed.FetchResult, error)
}

type AriaClient interface {
	GetVersion(ctx context.Context) (aria2.Version, error)
	AddTorrent(ctx context.Context, torrent []byte, gid, downloadDir string) (string, error)
	TellStatus(ctx context.Context, gid string) (aria2.Status, error)
	Pause(ctx context.Context, gid string) error
	Unpause(ctx context.Context, gid string) error
}

type AriaFactory func(endpoint, secret string) (AriaClient, error)

type Service struct {
	store       *store.Store
	feeds       FeedFetcher
	ariaFactory AriaFactory
	httpClient  *http.Client
	logger      *slog.Logger

	mu              sync.Mutex
	syncing         map[int64]struct{}
	processing      map[int64]struct{}
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	statusSemaphore chan struct{}
}

func New(store *store.Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store: store,
		ariaFactory: func(endpoint, secret string) (AriaClient, error) {
			return aria2.New(endpoint, secret)
		},
		logger:          logger,
		syncing:         make(map[int64]struct{}),
		processing:      make(map[int64]struct{}),
		statusSemaphore: make(chan struct{}, 4),
	}
}

func (s *Service) Start(parent context.Context) {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	s.ctx, s.cancel = context.WithCancel(parent)
	s.mu.Unlock()

	s.wg.Add(3)
	go s.subscriptionLoop()
	go s.downloadLoop()
	go s.sessionCleanupLoop()
}

func (s *Service) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

func (s *Service) TriggerSync(id int64) bool {
	if !s.beginSync(id) {
		return false
	}
	s.mu.Lock()
	ctx := s.ctx
	s.mu.Unlock()
	if ctx == nil {
		s.endSync(id)
		return false
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.endSync(id)
		if err := s.syncSubscription(ctx, id); err != nil && !errors.Is(err, context.Canceled) {
			s.logger.Warn("subscription sync failed", "subscription_id", id, "error", err)
		}
	}()
	return true
}

func (s *Service) SyncSubscription(ctx context.Context, id int64) error {
	if !s.beginSync(id) {
		return ErrBusy
	}
	defer s.endSync(id)
	return s.syncSubscription(ctx, id)
}

func (s *Service) TestAria2(ctx context.Context, settings domain.Settings) (aria2.Version, error) {
	client, err := s.ariaFactory(settings.Aria2URL, settings.Aria2Secret)
	if err != nil {
		return aria2.Version{}, err
	}
	return client.GetVersion(ctx)
}

func (s *Service) PauseDownload(ctx context.Context, id int64) error {
	download, settings, client, err := s.downloadClient(ctx, id)
	if err != nil {
		return err
	}
	_ = settings
	if err := client.Pause(ctx, download.Aria2GID); err != nil {
		return err
	}
	return s.store.UpdateDownloadStatus(
		ctx,
		id,
		domain.DownloadPaused,
		download.TotalLength,
		download.CompletedLength,
		"",
	)
}

func (s *Service) ResumeDownload(ctx context.Context, id int64) error {
	download, settings, client, err := s.downloadClient(ctx, id)
	if err != nil {
		return err
	}
	_ = settings
	if err := client.Unpause(ctx, download.Aria2GID); err != nil {
		return err
	}
	return s.store.UpdateDownloadStatus(
		ctx,
		id,
		domain.DownloadWaiting,
		download.TotalLength,
		download.CompletedLength,
		"",
	)
}

func (s *Service) subscriptionLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	s.scheduleSubscriptions()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.scheduleSubscriptions()
		}
	}
}

func (s *Service) scheduleSubscriptions() {
	subscriptions, err := s.store.DueSubscriptions(s.ctx, 8)
	if err != nil {
		s.logger.Error("list due subscriptions", "error", err)
		return
	}
	for _, subscription := range subscriptions {
		s.TriggerSync(subscription.ID)
	}
}

func (s *Service) syncSubscription(ctx context.Context, id int64) error {
	subscription, err := s.store.Subscription(ctx, id)
	if err != nil {
		return err
	}
	if subscription.ArchivedAt != nil {
		return errors.New("archived subscriptions cannot be synchronized")
	}
	settings, err := s.store.Settings(ctx)
	if err != nil {
		return err
	}
	interval := time.Duration(settings.PollIntervalMinutes) * time.Minute
	fetcher := s.feeds
	if fetcher == nil {
		fetcher, err = feed.NewClientWithProxy(proxySettings(settings))
		if err != nil {
			retryAt := time.Now().UTC().Add(minDuration(interval, time.Minute))
			_ = s.store.MarkSyncError(ctx, id, err.Error(), retryAt)
			return err
		}
	}
	result, err := fetcher.Fetch(ctx, subscription.URL, subscription.ETag, subscription.LastModified)
	if err != nil {
		retryAt := time.Now().UTC().Add(minDuration(interval, time.Minute))
		_ = s.store.MarkSyncError(ctx, id, err.Error(), retryAt)
		return err
	}
	nextCheck := time.Now().UTC().Add(interval)
	if result.NotModified {
		return s.store.MarkSyncNotModified(
			ctx,
			id,
			result.ETag,
			result.LastModified,
			nextCheck,
		)
	}
	return s.store.ApplyFeedSync(
		ctx,
		id,
		result.Title,
		result.ETag,
		result.LastModified,
		result.Items,
		nextCheck,
	)
}

func (s *Service) downloadLoop() {
	defer s.wg.Done()
	submitTicker := time.NewTicker(5 * time.Second)
	statusTicker := time.NewTicker(5 * time.Second)
	defer submitTicker.Stop()
	defer statusTicker.Stop()
	s.scheduleDownloads()
	s.trackDownloads()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-submitTicker.C:
			s.scheduleDownloads()
		case <-statusTicker.C:
			s.trackDownloads()
		}
	}
}

func (s *Service) scheduleDownloads() {
	downloads, err := s.store.PendingDownloads(s.ctx, 8)
	if err != nil {
		s.logger.Error("list pending downloads", "error", err)
		return
	}
	for _, download := range downloads {
		if !s.beginProcessing(download.ID) {
			continue
		}
		s.wg.Add(1)
		go func(download domain.Download) {
			defer s.wg.Done()
			defer s.endProcessing(download.ID)
			select {
			case s.statusSemaphore <- struct{}{}:
				defer func() { <-s.statusSemaphore }()
			case <-s.ctx.Done():
				return
			}
			s.submitDownload(s.ctx, download)
		}(download)
	}
}

func (s *Service) submitDownload(ctx context.Context, download domain.Download) {
	settings, err := s.store.Settings(ctx)
	if err != nil {
		s.retryDownload(ctx, download, err, false)
		return
	}
	client, err := s.ariaFactory(settings.Aria2URL, settings.Aria2Secret)
	if err != nil {
		s.retryDownload(ctx, download, err, false)
		return
	}
	current, err := client.TellStatus(ctx, download.Aria2GID)
	if err == nil {
		status, total, completed, message := aria2.MapStatus(current)
		if updateErr := s.store.UpdateDownloadStatus(
			ctx, download.ID, status, total, completed, message,
		); updateErr != nil {
			s.logger.Error("adopt existing aria2 download", "download_id", download.ID, "error", updateErr)
		}
		return
	}
	if !aria2.IsGIDNotFound(err) {
		s.retryDownload(ctx, download, err, false)
		return
	}
	if err := s.store.MarkDownloadSubmitting(ctx, download.ID); err != nil {
		s.logger.Error("mark download submitting", "download_id", download.ID, "error", err)
		return
	}
	torrent, permanent, err := s.fetchTorrent(ctx, download.EnclosureURL, settings)
	if err != nil {
		s.retryDownload(ctx, download, err, permanent)
		return
	}
	override, err := s.store.DownloadDirectory(ctx, download.ID)
	if err != nil {
		s.retryDownload(ctx, download, err, true)
		return
	}
	directory := strings.TrimSpace(override)
	if directory == "" {
		directory = strings.TrimSpace(settings.DefaultDownloadDir)
	}
	gid, err := client.AddTorrent(ctx, torrent, download.Aria2GID, directory)
	if err != nil {
		permanent = isPermanentRPCError(err)
		s.retryDownload(ctx, download, err, permanent)
		return
	}
	if gid != download.Aria2GID {
		s.retryDownload(
			ctx,
			download,
			fmt.Errorf("aria2 returned unexpected GID %q", gid),
			true,
		)
		return
	}
	if err := s.store.UpdateDownloadStatus(
		ctx, download.ID, domain.DownloadWaiting, 0, 0, "",
	); err != nil {
		s.logger.Error("record submitted download", "download_id", download.ID, "error", err)
	}
}

func (s *Service) trackDownloads() {
	downloads, err := s.store.TrackableDownloads(s.ctx, 100)
	if err != nil {
		s.logger.Error("list aria2 downloads", "error", err)
		return
	}
	if len(downloads) == 0 {
		return
	}
	settings, err := s.store.Settings(s.ctx)
	if err != nil {
		s.logger.Error("load aria2 settings", "error", err)
		return
	}
	client, err := s.ariaFactory(settings.Aria2URL, settings.Aria2Secret)
	if err != nil {
		s.logger.Warn("create aria2 client", "error", err)
		return
	}
	for _, download := range downloads {
		if !s.beginProcessing(download.ID) {
			continue
		}
		s.wg.Add(1)
		go func(download domain.Download) {
			defer s.wg.Done()
			defer s.endProcessing(download.ID)
			select {
			case s.statusSemaphore <- struct{}{}:
				defer func() { <-s.statusSemaphore }()
			case <-s.ctx.Done():
				return
			}
			status, err := client.TellStatus(s.ctx, download.Aria2GID)
			if err != nil {
				if aria2.IsGIDNotFound(err) {
					if download.Status == domain.DownloadSubmitting {
						_, _ = s.store.ResetDownload(s.ctx, download.ID)
					} else {
						_ = s.store.UpdateDownloadStatus(
							s.ctx,
							download.ID,
							domain.DownloadUnknown,
							download.TotalLength,
							download.CompletedLength,
							"aria2 no longer knows this GID",
						)
					}
				} else {
					_ = s.store.MarkDownloadCheckError(s.ctx, download.ID, err.Error())
				}
				return
			}
			mapped, total, completed, message := aria2.MapStatus(status)
			if err := s.store.UpdateDownloadStatus(
				s.ctx, download.ID, mapped, total, completed, message,
			); err != nil {
				s.logger.Error("update aria2 status", "download_id", download.ID, "error", err)
			}
		}(download)
	}
}

func (s *Service) sessionCleanupLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if err := s.store.CleanupSessions(s.ctx); err != nil {
				s.logger.Warn("cleanup sessions", "error", err)
			}
		}
	}
}

func (s *Service) fetchTorrent(
	ctx context.Context,
	rawURL string,
	settings domain.Settings,
) ([]byte, bool, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, true, errors.New("invalid torrent URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, true, fmt.Errorf("create torrent request: %w", err)
	}
	request.Header.Set("Accept", "application/x-bittorrent, application/octet-stream;q=0.9")
	request.Header.Set("User-Agent", "rssdownload/1.0")
	client := s.httpClient
	if client == nil {
		transport, transportErr := proxyconfig.NewTransport(proxySettings(settings))
		if transportErr != nil {
			return nil, true, transportErr
		}
		client = &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				req.Header.Set("User-Agent", "rssdownload/1.0")
				return nil
			},
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, false, fmt.Errorf("download torrent: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		permanent := response.StatusCode >= 400 && response.StatusCode < 500 &&
			response.StatusCode != http.StatusRequestTimeout &&
			response.StatusCode != http.StatusTooManyRequests
		return nil, permanent, fmt.Errorf("download torrent: HTTP %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxTorrentSize+1))
	if err != nil {
		return nil, false, fmt.Errorf("read torrent: %w", err)
	}
	if len(data) > maxTorrentSize {
		return nil, true, fmt.Errorf("torrent exceeds %d bytes", maxTorrentSize)
	}
	if len(data) == 0 || data[0] != 'd' {
		return nil, true, errors.New("response is not a valid bencoded torrent dictionary")
	}
	return data, false, nil
}

func proxySettings(settings domain.Settings) proxyconfig.Config {
	return proxyconfig.Config{
		URL:      settings.ProxyURL,
		Username: settings.ProxyUsername,
		Password: settings.ProxyPassword,
	}
}

func (s *Service) retryDownload(
	ctx context.Context,
	download domain.Download,
	err error,
	permanent bool,
) {
	delay := retryDelay(download.RetryCount)
	if storeErr := s.store.MarkDownloadRetry(
		ctx,
		download.ID,
		err.Error(),
		time.Now().UTC().Add(delay),
		permanent,
	); storeErr != nil {
		s.logger.Error(
			"record download failure",
			"download_id", download.ID,
			"error", storeErr,
			"cause", err,
		)
	}
}

func (s *Service) downloadClient(
	ctx context.Context,
	id int64,
) (domain.Download, domain.Settings, AriaClient, error) {
	download, err := s.store.Download(ctx, id)
	if err != nil {
		return domain.Download{}, domain.Settings{}, nil, err
	}
	settings, err := s.store.Settings(ctx)
	if err != nil {
		return domain.Download{}, domain.Settings{}, nil, err
	}
	client, err := s.ariaFactory(settings.Aria2URL, settings.Aria2Secret)
	if err != nil {
		return domain.Download{}, domain.Settings{}, nil, err
	}
	return download, settings, client, nil
}

func (s *Service) beginSync(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.syncing[id]; exists {
		return false
	}
	s.syncing[id] = struct{}{}
	return true
}

func (s *Service) endSync(id int64) {
	s.mu.Lock()
	delete(s.syncing, id)
	s.mu.Unlock()
}

func (s *Service) beginProcessing(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.processing[id]; exists {
		return false
	}
	s.processing[id] = struct{}{}
	return true
}

func (s *Service) endProcessing(id int64) {
	s.mu.Lock()
	delete(s.processing, id)
	s.mu.Unlock()
}

func retryDelay(retryCount int) time.Duration {
	switch retryCount {
	case 0:
		return time.Minute
	case 1:
		return 5 * time.Minute
	case 2:
		return 15 * time.Minute
	default:
		return time.Hour
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func isPermanentRPCError(err error) bool {
	var rpcErr *aria2.RPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	message := strings.ToLower(rpcErr.Message)
	return strings.Contains(message, "torrent") ||
		strings.Contains(message, "option") ||
		strings.Contains(message, "bad request")
}
