package feed

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/Rehtt/rssdownload/internal/domain"
	proxyconfig "github.com/Rehtt/rssdownload/internal/proxy"
)

func TestValidateMikanURL(t *testing.T) {
	t.Parallel()
	valid := "https://mikanani.me/RSS/MyBangumi?token=secret"
	if err := ValidateMikanURL(valid); err != nil {
		t.Fatalf("expected valid URL: %v", err)
	}
	tests := []string{
		"http://mikanani.me/RSS/MyBangumi?token=secret",
		"https://example.com/RSS/MyBangumi?token=secret",
		"https://mikanani.me/RSS/Bangumi?token=secret",
		"https://mikanani.me/RSS/MyBangumi",
	}
	for _, value := range tests {
		if err := ValidateMikanURL(value); err == nil {
			t.Errorf("expected %q to be rejected", value)
		}
	}
	if redacted := RedactURL(valid); redacted != "https://mikanani.me/RSS/MyBangumi?token=REDACTED" {
		t.Fatalf("unexpected redacted URL: %s", redacted)
	}
}

func TestFetchUsesConditionalHeaders(t *testing.T) {
	t.Parallel()
	client := NewClientWithHTTP(&http.Client{
		Transport: feedRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("If-None-Match") != `"etag"` {
				t.Errorf("missing If-None-Match header")
			}
			if request.Header.Get("If-Modified-Since") == "" {
				t.Errorf("missing If-Modified-Since header")
			}
			return &http.Response{
				StatusCode: http.StatusNotModified,
				Status:     "304 Not Modified",
				Header: http.Header{
					"ETag":          []string{`"etag"`},
					"Last-Modified": []string{"Tue, 28 Jul 2026 10:00:00 GMT"},
				},
				Body: io.NopCloser(bytes.NewReader(nil)),
			}, nil
		}),
	})
	result, err := client.Fetch(
		context.Background(),
		"https://mikanani.me/RSS/MyBangumi?token=secret",
		`"etag"`,
		"Tue, 28 Jul 2026 10:00:00 GMT",
	)
	if err != nil {
		t.Fatalf("fetch feed: %v", err)
	}
	if !result.NotModified {
		t.Fatal("expected a not-modified result")
	}
}

func TestNewClientWithProxyConfiguresFeedTransport(t *testing.T) {
	t.Parallel()

	client, err := NewClientWithProxy(proxyconfig.Config{
		URL:      "http://proxy.example:7890",
		Username: "proxy-user",
		Password: "proxy-pass",
	})
	if err != nil {
		t.Fatalf("create feed client: %v", err)
	}
	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type: %T", client.httpClient.Transport)
	}
	request, err := http.NewRequest(http.MethodGet, "https://mikanani.me/", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	proxyURL, err := transport.Proxy(request)
	if err != nil {
		t.Fatalf("resolve proxy: %v", err)
	}
	if proxyURL == nil || proxyURL.Host != "proxy.example:7890" {
		t.Fatalf("unexpected proxy URL: %#v", proxyURL)
	}
}

func TestParseMikanRSS(t *testing.T) {
	t.Parallel()
	const document = `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0">
  <channel>
    <title>Mikan Project - 我的番组</title>
    <item>
      <guid isPermaLink="false">release-one</guid>
      <link>https://mikanani.me/Home/Episode/abcdef</link>
      <title>[Group] Example - 01 [1080p]</title>
      <description>Example release [512 MB]</description>
      <torrent xmlns="https://mikanani.me/0.1/">
        <link>https://mikanani.me/Home/Episode/abcdef</link>
        <contentLength>536870912</contentLength>
        <pubDate>2026-07-28T10:20:30.123</pubDate>
      </torrent>
      <enclosure type="application/x-bittorrent" length="536870912"
        url="https://mikanani.me/Download/20260728/abcdef.torrent"/>
    </item>
    <item>
      <guid>unsupported</guid>
      <title>No enclosure</title>
      <pubDate>Mon, 28 Jul 2026 10:20:30 +0000</pubDate>
    </item>
  </channel>
</rss>`
	title, items, err := Parse([]byte(document))
	if err != nil {
		t.Fatalf("parse RSS: %v", err)
	}
	if title != "Mikan Project - 我的番组" {
		t.Fatalf("unexpected title: %q", title)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	first := items[0]
	if first.RecognitionStatus != domain.RecognitionRecognized {
		t.Fatalf("expected recognized item, got %s", first.RecognitionStatus)
	}
	if first.EnclosureLength != 536870912 {
		t.Fatalf("unexpected enclosure length: %d", first.EnclosureLength)
	}
	if first.PublishedAt == nil || first.PublishedAt.Year() != 2026 {
		t.Fatalf("unexpected publication date: %v", first.PublishedAt)
	}
	if first.ItemKey == "" || first.ReleaseKey == "" {
		t.Fatal("fingerprints must be populated")
	}
	if items[1].RecognitionStatus != domain.RecognitionUnsupported {
		t.Fatalf("expected unsupported item, got %s", items[1].RecognitionStatus)
	}
	if items[1].RecognitionError == "" {
		t.Fatal("unsupported item should include a reason")
	}
}

func TestParseRejectsMalformedXML(t *testing.T) {
	t.Parallel()
	if _, _, err := Parse([]byte(`<rss><channel>`)); err == nil {
		t.Fatal("expected malformed XML error")
	}
}

type feedRoundTripFunc func(*http.Request) (*http.Response, error)

func (function feedRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
