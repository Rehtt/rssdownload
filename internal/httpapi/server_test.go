package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Rehtt/rssdownload/internal/app"
	"github.com/Rehtt/rssdownload/internal/auth"
	"github.com/Rehtt/rssdownload/internal/database"
	"github.com/Rehtt/rssdownload/internal/store"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	db, _, err := database.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	dataStore := store.New(db)
	authService := auth.New(dataStore)
	if err := authService.Bootstrap(context.Background(), "admin", "password123"); err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	appService := app.New(dataStore, logger)
	return New(dataStore, authService, appService, logger, false, nil).Handler()
}

func TestAuthenticationAndCSRF(t *testing.T) {
	t.Parallel()
	handler := newTestServer(t)

	loginBody := bytes.NewBufferString(`{"username":"admin","password":"password123"}`)
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", loginBody)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	var loginResult struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&loginResult); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if loginResult.CSRFToken == "" {
		t.Fatal("login did not return a CSRF token")
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected a session cookie, got %d", len(cookies))
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions", nil)
	getRequest.AddCookie(cookies[0])
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("authenticated GET status = %d", getResponse.Code)
	}

	postBody := bytes.NewBufferString(
		`{"url":"https://mikanani.me/RSS/MyBangumi?token=secret","downloadExisting":false}`,
	)
	postRequest := httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions", postBody)
	postRequest.Header.Set("Content-Type", "application/json")
	postRequest.AddCookie(cookies[0])
	postResponse := httptest.NewRecorder()
	handler.ServeHTTP(postResponse, postRequest)
	if postResponse.Code != http.StatusForbidden {
		t.Fatalf("POST without CSRF status = %d", postResponse.Code)
	}

	postBody = bytes.NewBufferString(
		`{"url":"https://mikanani.me/RSS/MyBangumi?token=secret","downloadExisting":false}`,
	)
	postRequest = httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions", postBody)
	postRequest.Header.Set("Content-Type", "application/json")
	postRequest.Header.Set("X-CSRF-Token", loginResult.CSRFToken)
	postRequest.AddCookie(cookies[0])
	postResponse = httptest.NewRecorder()
	handler.ServeHTTP(postResponse, postRequest)
	if postResponse.Code != http.StatusCreated {
		t.Fatalf("POST with CSRF status = %d, body=%s", postResponse.Code, postResponse.Body.String())
	}
	if bytes.Contains(postResponse.Body.Bytes(), []byte("token=secret")) {
		t.Fatal("subscription response leaked the RSS token")
	}
}

func TestHealthIsPublic(t *testing.T) {
	t.Parallel()
	handler := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("health status = %d", response.Code)
	}
}

func TestProxySettingsPasswordIsWriteOnly(t *testing.T) {
	t.Parallel()
	handler := newTestServer(t)

	loginBody := bytes.NewBufferString(`{"username":"admin","password":"password123"}`)
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", loginBody)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	var loginResult struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&loginResult); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected a session cookie, got %d", len(cookies))
	}

	updateBody := bytes.NewBufferString(`{
		"aria2Url":"http://127.0.0.1:6800/jsonrpc",
		"proxyUrl":"socks5://proxy.example:1080",
		"proxyUsername":"proxy-user",
		"proxyPassword":"proxy-pass",
		"defaultDownloadDir":"",
		"pollIntervalMinutes":10
	}`)
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/settings", updateBody)
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set("X-CSRF-Token", loginResult.CSRFToken)
	updateRequest.AddCookie(cookies[0])
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d, body=%s", updateResponse.Code, updateResponse.Body.String())
	}
	if bytes.Contains(updateResponse.Body.Bytes(), []byte("proxy-pass")) {
		t.Fatal("settings response leaked the proxy password")
	}
	var updated struct {
		ProxyURL         string `json:"proxyUrl"`
		ProxyUsername    string `json:"proxyUsername"`
		ProxyPassword    string `json:"proxyPassword"`
		HasProxyPassword bool   `json:"hasProxyPassword"`
	}
	if err := json.NewDecoder(updateResponse.Body).Decode(&updated); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if updated.ProxyURL != "socks5://proxy.example:1080" ||
		updated.ProxyUsername != "proxy-user" ||
		updated.ProxyPassword != "••••" ||
		!updated.HasProxyPassword {
		t.Fatalf("unexpected public proxy settings: %#v", updated)
	}

	preserveBody := bytes.NewBufferString(`{
		"aria2Url":"http://127.0.0.1:6800/jsonrpc",
		"proxyUrl":"socks5://proxy.example:1080",
		"proxyUsername":"proxy-user",
		"proxyPassword":"",
		"defaultDownloadDir":"",
		"pollIntervalMinutes":10
	}`)
	preserveRequest := httptest.NewRequest(http.MethodPut, "/api/v1/settings", preserveBody)
	preserveRequest.Header.Set("Content-Type", "application/json")
	preserveRequest.Header.Set("X-CSRF-Token", loginResult.CSRFToken)
	preserveRequest.AddCookie(cookies[0])
	preserveResponse := httptest.NewRecorder()
	handler.ServeHTTP(preserveResponse, preserveRequest)
	if preserveResponse.Code != http.StatusOK {
		t.Fatalf("preserve status = %d, body=%s", preserveResponse.Code, preserveResponse.Body.String())
	}
	if !bytes.Contains(preserveResponse.Body.Bytes(), []byte(`"hasProxyPassword":true`)) {
		t.Fatalf("blank password did not preserve the stored value: %s", preserveResponse.Body.String())
	}
}

func TestProxySettingsRejectInvalidURL(t *testing.T) {
	t.Parallel()
	handler := newTestServer(t)

	loginBody := bytes.NewBufferString(`{"username":"admin","password":"password123"}`)
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", loginBody)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	var loginResult struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&loginResult); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected a session cookie, got %d", len(cookies))
	}

	updateBody := bytes.NewBufferString(`{
		"aria2Url":"http://127.0.0.1:6800/jsonrpc",
		"proxyUrl":"ftp://proxy.example:21",
		"defaultDownloadDir":"",
		"pollIntervalMinutes":10
	}`)
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/settings", updateBody)
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set("X-CSRF-Token", loginResult.CSRFToken)
	updateRequest.AddCookie(cookies[0])
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusBadRequest ||
		!bytes.Contains(updateResponse.Body.Bytes(), []byte(`"code":"invalid_proxy"`)) {
		t.Fatalf("invalid proxy status = %d, body=%s", updateResponse.Code, updateResponse.Body.String())
	}
}
