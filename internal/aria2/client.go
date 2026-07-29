package aria2

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Rehtt/rssdownload/internal/domain"
)

const maxRPCResponse = 2 << 20

type Client struct {
	endpoint   string
	secret     string
	httpClient *http.Client
	requestID  atomic.Uint64
}

type Version struct {
	Version         string   `json:"version"`
	EnabledFeatures []string `json:"enabledFeatures"`
}

type Status struct {
	GID             string `json:"gid"`
	Status          string `json:"status"`
	TotalLength     string `json:"totalLength"`
	CompletedLength string `json:"completedLength"`
	DownloadSpeed   string `json:"downloadSpeed"`
	ErrorCode       string `json:"errorCode"`
	ErrorMessage    string `json:"errorMessage"`
	Dir             string `json:"dir"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("aria2 RPC error %d: %s", e.Code, e.Message)
}

func New(endpoint, secret string) (*Client, error) {
	return NewWithHTTP(endpoint, secret, &http.Client{Timeout: 15 * time.Second})
}

func NewWithHTTP(endpoint, secret string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid aria2 URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("aria2 URL must use HTTP or HTTPS")
	}
	if parsed.Host == "" {
		return nil, errors.New("aria2 URL must include a host")
	}
	if parsed.User != nil {
		return nil, errors.New("aria2 URL must not include user information")
	}
	if httpClient == nil {
		return nil, errors.New("HTTP client must not be nil")
	}
	return &Client{
		endpoint:   endpoint,
		secret:     secret,
		httpClient: httpClient,
	}, nil
}

func (c *Client) GetVersion(ctx context.Context) (Version, error) {
	var result Version
	if err := c.call(ctx, "aria2.getVersion", nil, &result); err != nil {
		return Version{}, err
	}
	return result, nil
}

func (c *Client) AddTorrent(
	ctx context.Context,
	torrent []byte,
	gid, downloadDir string,
) (string, error) {
	options := map[string]string{"gid": gid}
	if strings.TrimSpace(downloadDir) != "" {
		options["dir"] = strings.TrimSpace(downloadDir)
	}
	params := []any{
		base64.StdEncoding.EncodeToString(torrent),
		[]string{},
		options,
	}
	var result string
	if err := c.call(ctx, "aria2.addTorrent", params, &result); err != nil {
		return "", err
	}
	return result, nil
}

func (c *Client) TellStatus(ctx context.Context, gid string) (Status, error) {
	keys := []string{
		"gid",
		"status",
		"totalLength",
		"completedLength",
		"downloadSpeed",
		"errorCode",
		"errorMessage",
		"dir",
	}
	var result Status
	if err := c.call(ctx, "aria2.tellStatus", []any{gid, keys}, &result); err != nil {
		return Status{}, err
	}
	return result, nil
}

func (c *Client) Pause(ctx context.Context, gid string) error {
	var result string
	return c.call(ctx, "aria2.pause", []any{gid}, &result)
}

func (c *Client) Unpause(ctx context.Context, gid string) error {
	var result string
	return c.call(ctx, "aria2.unpause", []any{gid}, &result)
}

func (c *Client) call(ctx context.Context, method string, params []any, result any) error {
	if params == nil {
		params = []any{}
	}
	if c.secret != "" {
		params = append([]any{"token:" + c.secret}, params...)
	}
	id := strconv.FormatUint(c.requestID.Add(1), 10)
	payload := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode aria2 request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create aria2 request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "rssdownload/1.0")
	response, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call aria2: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxRPCResponse+1))
	if err != nil {
		return fmt.Errorf("read aria2 response: %w", err)
	}
	if len(responseBody) > maxRPCResponse {
		return errors.New("aria2 response is too large")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("aria2 returned HTTP %s", response.Status)
	}
	var decoded rpcResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return fmt.Errorf("decode aria2 response: %w", err)
	}
	if decoded.ID != "" && decoded.ID != id {
		return errors.New("aria2 returned a mismatched request id")
	}
	if decoded.Error != nil {
		return decoded.Error
	}
	if result == nil {
		return nil
	}
	if len(decoded.Result) == 0 {
		return errors.New("aria2 response has no result")
	}
	if err := json.Unmarshal(decoded.Result, result); err != nil {
		return fmt.Errorf("decode aria2 result: %w", err)
	}
	return nil
}

func MapStatus(status Status) (domain.DownloadStatus, int64, int64, string) {
	total, _ := strconv.ParseInt(status.TotalLength, 10, 64)
	completed, _ := strconv.ParseInt(status.CompletedLength, 10, 64)
	message := status.ErrorMessage
	switch status.Status {
	case "active":
		return domain.DownloadActive, total, completed, message
	case "waiting":
		return domain.DownloadWaiting, total, completed, message
	case "paused":
		return domain.DownloadPaused, total, completed, message
	case "complete":
		return domain.DownloadComplete, total, completed, ""
	case "error":
		return domain.DownloadError, total, completed, message
	case "removed":
		return domain.DownloadUnknown, total, completed, "download was removed from aria2"
	default:
		return domain.DownloadUnknown, total, completed, "unknown aria2 status: " + status.Status
	}
}

func IsGIDNotFound(err error) bool {
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	message := strings.ToLower(rpcErr.Message)
	return strings.Contains(message, "gid") &&
		(strings.Contains(message, "not found") || strings.Contains(message, "not exist"))
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
}
