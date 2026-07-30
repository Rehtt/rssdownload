package aria2

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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

type AddTorrentOptions struct {
	GID             string
	DownloadDir     string
	SeedTimeMinutes *float64
	SeedRatio       *float64
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
	config AddTorrentOptions,
) (string, error) {
	options := map[string]string{"gid": config.GID}
	if strings.TrimSpace(config.DownloadDir) != "" {
		options["dir"] = strings.TrimSpace(config.DownloadDir)
	}
	if config.SeedTimeMinutes != nil {
		if !finiteNonNegative(*config.SeedTimeMinutes) {
			return "", errors.New("seed time must be a finite non-negative number")
		}
		options["seed-time"] = strconv.FormatFloat(*config.SeedTimeMinutes, 'f', -1, 64)
	}
	if config.SeedRatio != nil {
		if !finiteNonNegative(*config.SeedRatio) {
			return "", errors.New("seed ratio must be a finite non-negative number")
		}
		options["seed-ratio"] = strconv.FormatFloat(*config.SeedRatio, 'f', -1, 64)
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

func finiteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

var statusKeys = []string{
	"gid",
	"status",
	"totalLength",
	"completedLength",
	"downloadSpeed",
	"errorCode",
	"errorMessage",
	"dir",
}

func (c *Client) TellStatus(ctx context.Context, gid string) (Status, error) {
	var result Status
	if err := c.call(ctx, "aria2.tellStatus", []any{gid, statusKeys}, &result); err != nil {
		return Status{}, err
	}
	return result, nil
}

// StatusResult carries the outcome of one GID inside a batched status query.
type StatusResult struct {
	Status Status
	Err    error
}

// TellStatuses fetches the status of several GIDs in a single
// system.multicall round trip. The returned slice matches the order of gids;
// each entry holds either the decoded status or the per-GID RPC error.
func (c *Client) TellStatuses(ctx context.Context, gids []string) ([]StatusResult, error) {
	if len(gids) == 0 {
		return nil, nil
	}
	// system.multicall does not accept the secret token at the top level;
	// aria2 expects it as the first parameter of every nested call instead.
	calls := make([]map[string]any, 0, len(gids))
	for _, gid := range gids {
		params := make([]any, 0, 3)
		if c.secret != "" {
			params = append(params, "token:"+c.secret)
		}
		params = append(params, gid, statusKeys)
		calls = append(calls, map[string]any{
			"methodName": "aria2.tellStatus",
			"params":     params,
		})
	}
	var raw []json.RawMessage
	if err := c.rawCall(ctx, "system.multicall", []any{calls}, &raw); err != nil {
		return nil, err
	}
	if len(raw) != len(gids) {
		return nil, fmt.Errorf(
			"aria2 multicall returned %d results for %d calls",
			len(raw),
			len(gids),
		)
	}
	results := make([]StatusResult, len(gids))
	for index, entry := range raw {
		results[index] = decodeMulticallStatus(entry)
	}
	return results, nil
}

// decodeMulticallStatus unwraps one system.multicall entry: successful nested
// calls arrive as a one-element array, failed ones as a fault object with
// code and message.
func decodeMulticallStatus(entry json.RawMessage) StatusResult {
	var wrapped []json.RawMessage
	if err := json.Unmarshal(entry, &wrapped); err == nil {
		if len(wrapped) != 1 {
			return StatusResult{Err: errors.New("aria2 multicall entry has an unexpected shape")}
		}
		var status Status
		if err := json.Unmarshal(wrapped[0], &status); err != nil {
			return StatusResult{Err: fmt.Errorf("decode aria2 status: %w", err)}
		}
		return StatusResult{Status: status}
	}
	var fault RPCError
	if err := json.Unmarshal(entry, &fault); err != nil ||
		(fault.Code == 0 && fault.Message == "") {
		return StatusResult{Err: errors.New("aria2 multicall entry has an unexpected shape")}
	}
	return StatusResult{Err: &fault}
}

func (c *Client) Pause(ctx context.Context, gid string) error {
	var result string
	return c.call(ctx, "aria2.pause", []any{gid}, &result)
}

func (c *Client) Unpause(ctx context.Context, gid string) error {
	var result string
	return c.call(ctx, "aria2.unpause", []any{gid}, &result)
}

// RemoveDownloadResult drops a stopped download (complete/error/removed) from
// aria2's session so the same GID can be submitted again.
func (c *Client) RemoveDownloadResult(ctx context.Context, gid string) error {
	var result string
	return c.call(ctx, "aria2.removeDownloadResult", []any{gid}, &result)
}

func (c *Client) call(ctx context.Context, method string, params []any, result any) error {
	if params == nil {
		params = []any{}
	}
	if c.secret != "" {
		params = append([]any{"token:" + c.secret}, params...)
	}
	return c.rawCall(ctx, method, params, result)
}

// rawCall performs a JSON-RPC request with the parameters exactly as given,
// without prepending the secret token.
func (c *Client) rawCall(ctx context.Context, method string, params []any, result any) error {
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
	// aria2 reports JSON-RPC failures (e.g. "No such download for GID#x") with
	// non-2xx HTTP status codes, so extract the RPC error from the body before
	// falling back to a generic HTTP status error.
	var decoded rpcResponse
	decodeErr := json.Unmarshal(responseBody, &decoded)
	if decodeErr == nil && decoded.Error != nil {
		return decoded.Error
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("aria2 returned HTTP %s", response.Status)
	}
	if decodeErr != nil {
		return fmt.Errorf("decode aria2 response: %w", decodeErr)
	}
	if decoded.ID != "" && decoded.ID != id {
		return errors.New("aria2 returned a mismatched request id")
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
	// aria2 reports unknown GIDs as "No such download for GID#xxxx".
	if strings.Contains(message, "no such download") {
		return true
	}
	return strings.Contains(message, "gid") &&
		(strings.Contains(message, "not found") || strings.Contains(message, "not exist"))
}

// IsResultAlreadyRemoved reports whether aria2.removeDownloadResult failed
// only because the download result no longer exists. aria2 responds with
// "Could not remove download result of GID#xxxx" in that case (see
// RemoveDownloadResultRpcMethod in RpcMethodImpl.cc).
func IsResultAlreadyRemoved(err error) bool {
	if IsGIDNotFound(err) {
		return true
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	return strings.Contains(strings.ToLower(rpcErr.Message), "could not remove download result")
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
