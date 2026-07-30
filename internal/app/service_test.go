package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Rehtt/rssdownload/internal/aria2"
	"github.com/Rehtt/rssdownload/internal/database"
	"github.com/Rehtt/rssdownload/internal/domain"
	"github.com/Rehtt/rssdownload/internal/feed"
	"github.com/Rehtt/rssdownload/internal/store"
)

func testServiceStore(t *testing.T) *store.Store {
	t.Helper()
	db, _, err := database.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	return store.New(db)
}

func serviceItem(seed string) domain.ParsedItem {
	return domain.ParsedItem{
		ItemKey:           "item-" + seed,
		ReleaseKey:        "abcdef0123456789" + seed,
		GUID:              "guid-" + seed,
		Title:             "Release " + seed,
		EnclosureURL:      "https://mikanani.me/Download/" + seed + ".torrent",
		RecognitionStatus: domain.RecognitionRecognized,
	}
}

func TestSyncSubscriptionInitialBaseline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataStore := testServiceStore(t)
	subscription, err := dataStore.CreateSubscription(
		ctx,
		"",
		"https://mikanani.me/RSS/MyBangumi?token=secret",
		"",
		false,
	)
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	fetcher := &sequenceFetcher{results: []feed.FetchResult{
		{Title: "Mikan Feed", Items: []domain.ParsedItem{serviceItem("one")}},
		{Title: "Mikan Feed", Items: []domain.ParsedItem{serviceItem("one"), serviceItem("two")}},
	}}
	service := New(dataStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	service.feeds = fetcher
	if err := service.SyncSubscription(ctx, subscription.ID); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	page, err := dataStore.ListItems(ctx, store.ItemFilter{})
	if err != nil || page.Total != 1 || page.Items[0].Download != nil {
		t.Fatalf("unexpected initial baseline: %#v, %v", page, err)
	}
	if err := service.SyncSubscription(ctx, subscription.ID); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	page, err = dataStore.ListItems(ctx, store.ItemFilter{})
	if err != nil || page.Total != 2 {
		t.Fatalf("unexpected second page: %#v, %v", page, err)
	}
	var queued int
	for _, item := range page.Items {
		if item.Download != nil && item.Download.Status == domain.DownloadQueued {
			queued++
		}
	}
	if queued != 1 {
		t.Fatalf("expected one queued download, got %d", queued)
	}
	updated, err := dataStore.Subscription(ctx, subscription.ID)
	if err != nil || updated.Name != "Mikan Feed" {
		t.Fatalf("feed title was not adopted: %#v, %v", updated, err)
	}
}

func TestSubmitDownload(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataStore := testServiceStore(t)
	subscription, err := dataStore.CreateSubscription(
		ctx,
		"Feed",
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
		"Feed",
		"",
		"",
		[]domain.ParsedItem{serviceItem("one")},
		time.Now().Add(time.Minute),
	); err != nil {
		t.Fatalf("seed feed: %v", err)
	}
	pending, err := dataStore.PendingDownloads(ctx, 1)
	if err != nil || len(pending) != 1 {
		t.Fatalf("read pending download: %#v, %v", pending, err)
	}
	settings, err := dataStore.Settings(ctx)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	seedTime := 120.5
	seedRatio := 1.5
	settings.SeedTimeMinutes = &seedTime
	settings.SeedRatio = &seedRatio
	if _, err := dataStore.UpdateSettings(ctx, settings); err != nil {
		t.Fatalf("configure seeding: %v", err)
	}
	fake := &fakeAria{}
	service := New(dataStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	service.ariaFactory = func(_, _ string) (AriaClient, error) { return fake, nil }
	service.httpClient = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewBufferString("d4:infode")),
			}, nil
		}),
	}
	service.submitDownload(ctx, pending[0])
	updated, err := dataStore.Download(ctx, pending[0].ID)
	if err != nil {
		t.Fatalf("read submitted download: %v", err)
	}
	if updated.Status != domain.DownloadWaiting {
		t.Fatalf("expected waiting download, got %s (%s)", updated.Status, updated.ErrorMessage)
	}
	if fake.config.DownloadDir != "/anime" ||
		fake.config.GID != pending[0].Aria2GID ||
		fake.config.SeedTimeMinutes == nil ||
		*fake.config.SeedTimeMinutes != seedTime ||
		fake.config.SeedRatio == nil ||
		*fake.config.SeedRatio != seedRatio {
		t.Fatalf("unexpected aria2 submission: %#v", fake.config)
	}
}

func TestFetchTorrentUsesConfiguredProxy(t *testing.T) {
	t.Parallel()

	type proxyRequest struct {
		authorization string
		target        string
	}
	received := make(chan proxyRequest, 1)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		received <- proxyRequest{
			authorization: request.Header.Get("Proxy-Authorization"),
			target:        request.URL.String(),
		}
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("d4:infode"))
	}))
	t.Cleanup(proxyServer.Close)

	service := New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	torrent, permanent, err := service.fetchTorrent(
		context.Background(),
		"http://mikanani.me/Download/example.torrent",
		domain.Settings{
			ProxyURL:      proxyServer.URL,
			ProxyUsername: "proxy-user",
			ProxyPassword: "proxy-pass",
		},
	)
	if err != nil {
		t.Fatalf("fetch torrent through proxy: %v", err)
	}
	if permanent {
		t.Fatal("successful proxy request was marked permanent")
	}
	if string(torrent) != "d4:infode" {
		t.Fatalf("unexpected torrent body: %q", torrent)
	}
	proxied := <-received
	if proxied.target != "http://mikanani.me/Download/example.torrent" {
		t.Fatalf("proxy received unexpected target: %q", proxied.target)
	}
	if proxied.authorization != "Basic cHJveHktdXNlcjpwcm94eS1wYXNz" {
		t.Fatalf("proxy authorization was not forwarded: %q", proxied.authorization)
	}
}

type sequenceFetcher struct {
	results []feed.FetchResult
	index   int
}

func (f *sequenceFetcher) Fetch(context.Context, string, string, string) (feed.FetchResult, error) {
	if f.index >= len(f.results) {
		return feed.FetchResult{}, errors.New("no result")
	}
	result := f.results[f.index]
	f.index++
	return result, nil
}

type fakeAria struct {
	config aria2.AddTorrentOptions
}

func (f *fakeAria) GetVersion(context.Context) (aria2.Version, error) {
	return aria2.Version{Version: "1.37.0"}, nil
}

func (f *fakeAria) AddTorrent(
	_ context.Context,
	_ []byte,
	config aria2.AddTorrentOptions,
) (string, error) {
	f.config = config
	return config.GID, nil
}

func (f *fakeAria) TellStatus(context.Context, string) (aria2.Status, error) {
	return aria2.Status{}, &aria2.RPCError{Code: 1, Message: "No such download for GID#0123456789abcdef"}
}

func (f *fakeAria) Pause(context.Context, string) error   { return nil }
func (f *fakeAria) Unpause(context.Context, string) error { return nil }

func (f *fakeAria) RemoveDownloadResult(context.Context, string) error { return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
