package aria2

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/Rehtt/rssdownload/internal/domain"
)

func TestClientAddTorrentAndAuthentication(t *testing.T) {
	t.Parallel()
	var request rpcRequest
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		body, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result":  "0123456789abcdef",
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(body)),
		}, nil
	})}

	client, err := NewWithHTTP("http://aria2.test/jsonrpc", "secret", httpClient)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	seedTime := 120.5
	seedRatio := 1.5
	gid, err := client.AddTorrent(
		context.Background(),
		[]byte("d4:infode"),
		AddTorrentOptions{
			GID:             "0123456789abcdef",
			DownloadDir:     "/downloads/anime",
			SeedTimeMinutes: &seedTime,
			SeedRatio:       &seedRatio,
		},
	)
	if err != nil {
		t.Fatalf("add torrent: %v", err)
	}
	if gid != "0123456789abcdef" {
		t.Fatalf("unexpected GID: %s", gid)
	}
	if request.Method != "aria2.addTorrent" {
		t.Fatalf("unexpected method: %s", request.Method)
	}
	if request.Params[0] != "token:secret" {
		t.Fatalf("secret was not the first parameter: %#v", request.Params)
	}
	if request.Params[1] != base64.StdEncoding.EncodeToString([]byte("d4:infode")) {
		t.Fatalf("torrent was not base64 encoded: %#v", request.Params[1])
	}
	options, ok := request.Params[3].(map[string]any)
	if !ok {
		t.Fatalf("options have unexpected shape: %#v", request.Params[3])
	}
	if options["gid"] != "0123456789abcdef" ||
		options["dir"] != "/downloads/anime" ||
		options["seed-time"] != "120.5" ||
		options["seed-ratio"] != "1.5" {
		t.Fatalf("unexpected options: %#v", options)
	}
}

func TestClientAddTorrentOmitsUnsetSeedOptions(t *testing.T) {
	t.Parallel()
	var request rpcRequest
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		body, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result":  "0123456789abcdef",
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(body)),
		}, nil
	})}
	client, err := NewWithHTTP("http://aria2.test/jsonrpc", "", httpClient)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.AddTorrent(
		context.Background(),
		[]byte("d4:infode"),
		AddTorrentOptions{GID: "0123456789abcdef"},
	); err != nil {
		t.Fatalf("add torrent: %v", err)
	}
	options, ok := request.Params[2].(map[string]any)
	if !ok {
		t.Fatalf("options have unexpected shape: %#v", request.Params[2])
	}
	if _, exists := options["seed-time"]; exists {
		t.Fatalf("unset seed time was sent: %#v", options)
	}
	if _, exists := options["seed-ratio"]; exists {
		t.Fatalf("unset seed ratio was sent: %#v", options)
	}
}

func TestMapStatus(t *testing.T) {
	t.Parallel()
	status, total, completed, message := MapStatus(Status{
		Status:          "active",
		TotalLength:     "100",
		CompletedLength: "25",
	})
	if status != domain.DownloadActive || total != 100 || completed != 25 || message != "" {
		t.Fatalf("unexpected mapping: %s %d %d %q", status, total, completed, message)
	}
}

func TestRPCError(t *testing.T) {
	t.Parallel()
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		body, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"error": map[string]any{
				"code":    1,
				"message": "No such download for GID#0123456789abcdef",
			},
		})
		// aria2 pairs RPC error bodies with HTTP 400 (see
		// HttpServerBodyCommand.cc), so mirror the real behaviour here.
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Status:     "400 Bad Request",
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(body)),
		}, nil
	})}
	client, err := NewWithHTTP("http://aria2.test/jsonrpc", "", httpClient)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.TellStatus(context.Background(), "0123")
	if !IsGIDNotFound(err) {
		t.Fatalf("expected GID-not-found error, got %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
