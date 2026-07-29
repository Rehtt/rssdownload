package feed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Rehtt/rssdownload/internal/domain"
	proxyconfig "github.com/Rehtt/rssdownload/internal/proxy"
)

const (
	maxFeedSize = 5 << 20
	userAgent   = "rssdownload/1.0 (+https://github.com/Rehtt/rssdownload)"
)

type FetchResult struct {
	Title        string
	Items        []domain.ParsedItem
	ETag         string
	LastModified string
	NotModified  bool
}

type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	client, err := NewClientWithProxy(proxyconfig.Config{})
	if err != nil {
		panic(err)
	}
	return client
}

func NewClientWithProxy(config proxyconfig.Config) (*Client, error) {
	transport, err := proxyconfig.NewTransport(config)
	if err != nil {
		return nil, err
	}
	return &Client{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				req.Header.Set("User-Agent", userAgent)
				return nil
			},
		},
	}, nil
}

func NewClientWithHTTP(client *http.Client) *Client {
	return &Client{httpClient: client}
}

func ValidateMikanURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid RSS URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return errors.New("Mikan RSS URL must use HTTPS")
	}
	if !strings.EqualFold(parsed.Hostname(), "mikanani.me") {
		return errors.New("only mikanani.me RSS subscriptions are supported")
	}
	if parsed.User != nil {
		return errors.New("RSS URL must not contain user information")
	}
	if path.Clean(parsed.Path) != "/RSS/MyBangumi" {
		return errors.New("only /RSS/MyBangumi subscriptions are supported")
	}
	if strings.TrimSpace(parsed.Query().Get("token")) == "" {
		return errors.New("Mikan RSS URL must contain a non-empty token")
	}
	if parsed.Fragment != "" {
		return errors.New("RSS URL must not contain a fragment")
	}
	return nil
}

func RedactURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "••••"
	}
	query := parsed.Query()
	if query.Has("token") {
		query.Set("token", "REDACTED")
		parsed.RawQuery = query.Encode()
	}
	return parsed.String()
}

func (c *Client) Fetch(
	ctx context.Context,
	rawURL, etag, lastModified string,
) (FetchResult, error) {
	if err := ValidateMikanURL(rawURL); err != nil {
		return FetchResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return FetchResult{}, fmt.Errorf("create RSS request: %w", err)
	}
	req.Header.Set("Accept", "application/rss+xml, application/xml;q=0.9, text/xml;q=0.8")
	req.Header.Set("User-Agent", userAgent)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	response, err := c.httpClient.Do(req)
	if err != nil {
		return FetchResult{}, fmt.Errorf("fetch Mikan RSS: %w", err)
	}
	defer response.Body.Close()
	result := FetchResult{
		ETag:         response.Header.Get("ETag"),
		LastModified: response.Header.Get("Last-Modified"),
	}
	if result.ETag == "" {
		result.ETag = etag
	}
	if result.LastModified == "" {
		result.LastModified = lastModified
	}
	if response.StatusCode == http.StatusNotModified {
		result.NotModified = true
		return result, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return FetchResult{}, fmt.Errorf("fetch Mikan RSS: HTTP %s", response.Status)
	}
	limited := io.LimitReader(response.Body, maxFeedSize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return FetchResult{}, fmt.Errorf("read Mikan RSS: %w", err)
	}
	if len(body) > maxFeedSize {
		return FetchResult{}, fmt.Errorf("Mikan RSS exceeds %d bytes", maxFeedSize)
	}
	title, items, err := Parse(body)
	if err != nil {
		return FetchResult{}, err
	}
	result.Title = title
	result.Items = items
	return result, nil
}

type rssDocument struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title string    `xml:"title"`
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	GUID        string       `xml:"guid"`
	Title       string       `xml:"title"`
	Description string       `xml:"description"`
	Link        string       `xml:"link"`
	PubDate     string       `xml:"pubDate"`
	Enclosure   rssEnclosure `xml:"enclosure"`
	Torrent     rssTorrent   `xml:"torrent"`
}

type rssEnclosure struct {
	URL    string `xml:"url,attr"`
	Type   string `xml:"type,attr"`
	Length string `xml:"length,attr"`
}

type rssTorrent struct {
	Link          string `xml:"link"`
	ContentLength string `xml:"contentLength"`
	PubDate       string `xml:"pubDate"`
}

func Parse(data []byte) (string, []domain.ParsedItem, error) {
	var document rssDocument
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	decoder.Strict = true
	if err := decoder.Decode(&document); err != nil {
		return "", nil, fmt.Errorf("parse Mikan RSS XML: %w", err)
	}
	title := strings.TrimSpace(document.Channel.Title)
	items := make([]domain.ParsedItem, 0, len(document.Channel.Items))
	for _, raw := range document.Channel.Items {
		item := parseItem(raw)
		items = append(items, item)
	}
	return title, items, nil
}

func parseItem(raw rssItem) domain.ParsedItem {
	enclosureURL := strings.TrimSpace(raw.Enclosure.URL)
	title := strings.TrimSpace(raw.Title)
	guid := strings.TrimSpace(raw.GUID)
	detailURL := strings.TrimSpace(raw.Link)
	if detailURL == "" {
		detailURL = strings.TrimSpace(raw.Torrent.Link)
	}
	if title == "" {
		title = guid
	}
	if title == "" {
		title = "(untitled)"
	}
	length := parseLength(raw.Enclosure.Length)
	if length == 0 {
		length = parseLength(raw.Torrent.ContentLength)
	}
	publishedAt := parseDate(firstNonEmpty(raw.PubDate, raw.Torrent.PubDate))
	recognition := domain.RecognitionRecognized
	recognitionError := ""
	if enclosureURL == "" {
		recognition = domain.RecognitionUnsupported
		recognitionError = "missing torrent enclosure URL"
	} else {
		parsed, err := url.Parse(enclosureURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			recognition = domain.RecognitionUnsupported
			recognitionError = "invalid torrent enclosure URL"
		} else if raw.Enclosure.Type != "" &&
			!strings.EqualFold(strings.TrimSpace(raw.Enclosure.Type), "application/x-bittorrent") &&
			!strings.HasSuffix(strings.ToLower(parsed.Path), ".torrent") {
			recognition = domain.RecognitionUnsupported
			recognitionError = "enclosure is not a BitTorrent file"
		}
	}
	itemSeed := firstNonEmpty(enclosureURL, guid, detailURL, title+"\x00"+firstNonEmpty(raw.PubDate, raw.Torrent.PubDate))
	itemKey := digest(itemSeed)
	releaseKey := ""
	if recognition == domain.RecognitionRecognized {
		releaseKey = digest(enclosureURL)
	}
	return domain.ParsedItem{
		ItemKey:           itemKey,
		ReleaseKey:        releaseKey,
		GUID:              guid,
		Title:             title,
		Description:       strings.TrimSpace(raw.Description),
		DetailURL:         detailURL,
		EnclosureURL:      enclosureURL,
		EnclosureLength:   length,
		PublishedAt:       publishedAt,
		RecognitionStatus: recognition,
		RecognitionError:  recognitionError,
	}
}

func parseLength(value string) int64 {
	result, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || result < 0 {
		return 0
	}
	return result
}

func parseDate(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			utc := parsed.UTC()
			return &utc
		}
	}
	return nil
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
