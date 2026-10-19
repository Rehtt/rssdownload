package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Rehtt/rssdownload/internal/auth"
	"github.com/Rehtt/rssdownload/internal/oidctest"
)

func request(t *testing.T, client *http.Client, location string) *http.Response {
	t.Helper()
	response, err := client.Get(location)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func TestOIDCFullLoginSessionAndLogout(t *testing.T) {
	p := oidctest.New(t)
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	cfg := p.Config()
	cfg.RedirectURL = server.URL + "/api/v1/auth/oidc/callback"
	server.Config.Handler = newTestServer(t, WithOIDC(auth.NewOIDC(cfg)))
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	methods := request(t, client, server.URL+"/api/v1/auth/methods")
	var enabled map[string]bool
	if err := json.NewDecoder(methods.Body).Decode(&enabled); err != nil {
		t.Fatal(err)
	}
	if !enabled["password"] || !enabled["oidc"] {
		t.Fatalf("methods = %v", enabled)
	}
	if p.DiscoveryCalls.Load() != 0 {
		t.Fatal("methods endpoint triggered discovery")
	}
	begin := request(t, client, server.URL+"/api/v1/auth/oidc/login")
	if begin.StatusCode != http.StatusFound {
		t.Fatalf("begin status = %d", begin.StatusCode)
	}
	cookies := begin.Cookies()
	if len(cookies) != 1 || cookies[0].Name != oidcCookie || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].MaxAge != 600 || cookies[0].Path != "/api/v1/auth/oidc" {
		t.Fatalf("transaction cookie = %#v", cookies)
	}
	provider := request(t, client, begin.Header.Get("Location"))
	callbackURL := provider.Header.Get("Location")
	callback := request(t, client, callbackURL)
	if callback.StatusCode != http.StatusSeeOther || callback.Header.Get("Location") != "/" {
		t.Fatalf("callback = %d %s", callback.StatusCode, callback.Header.Get("Location"))
	}
	var cleared, sessionSet bool
	for _, cookie := range callback.Cookies() {
		if cookie.Name == oidcCookie && cookie.MaxAge == -1 {
			cleared = true
		}
		if cookie.Name == sessionCookie && cookie.HttpOnly && cookie.SameSite == http.SameSiteLaxMode {
			sessionSet = true
		}
	}
	if !cleared || !sessionSet {
		t.Fatal("callback did not clear binding cookie and set session")
	}
	session := request(t, client, server.URL+"/api/v1/auth/session")
	var loggedIn struct {
		Username  string `json:"username"`
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.NewDecoder(session.Body).Decode(&loggedIn); err != nil {
		t.Fatal(err)
	}
	if session.StatusCode != http.StatusOK || loggedIn.Username != "admin" || loggedIn.CSRFToken == "" {
		t.Fatalf("session = %d %#v", session.StatusCode, loggedIn)
	}
	data := request(t, client, server.URL+"/api/v1/subscriptions")
	if data.StatusCode != http.StatusOK {
		t.Fatalf("protected read = %d", data.StatusCode)
	}
	logoutReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/auth/logout", nil)
	logout, err := client.Do(logoutReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = logout.Body.Close()
	if logout.StatusCode != http.StatusForbidden {
		t.Fatalf("logout without CSRF = %d", logout.StatusCode)
	}
	logoutReq, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/auth/logout", nil)
	logoutReq.Header.Set("X-CSRF-Token", loggedIn.CSRFToken)
	logout, err = client.Do(logoutReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = logout.Body.Close()
	if logout.StatusCode != http.StatusNoContent {
		t.Fatalf("logout with CSRF = %d", logout.StatusCode)
	}
	if got := request(t, client, server.URL+"/api/v1/auth/session").StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("session after logout = %d", got)
	}
	if got := request(t, client, callbackURL).Header.Get("Location"); got != "/login?oidc_error=oidc_invalid_state" {
		t.Fatalf("replay redirect = %s", got)
	}
}

func TestOIDCDisabledAndCallbackFailure(t *testing.T) {
	handler := newTestServer(t)
	for _, path := range []string{"/api/v1/auth/oidc/login", "/api/v1/auth/oidc/callback?state=secret&code=secret"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login?oidc_error=oidc_disabled" {
			t.Fatalf("disabled response = %d %s", response.Code, response.Header().Get("Location"))
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/auth/methods", nil))
	if strings.TrimSpace(response.Body.String()) != `{"oidc":false,"password":true}` {
		t.Fatalf("disabled methods: %s", response.Body.String())
	}
}

func TestOIDCProviderFailureKeepsPasswordLogin(t *testing.T) {
	p := oidctest.New(t)
	p.FailDiscovery = true
	handler := newTestServer(t, WithOIDC(auth.NewOIDC(p.Config())))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login", nil))
	if got := response.Header().Get("Location"); got != "/login?oidc_error=oidc_unavailable" {
		t.Fatalf("provider failure = %s", got)
	}
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"password123"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("password login during outage = %d", login.Code)
	}
}

func TestOIDCCallbackSanitizesErrors(t *testing.T) {
	p := oidctest.New(t)
	s := auth.NewOIDC(p.Config())
	handler := newTestServer(t, WithOIDC(s))
	var logs bytes.Buffer
	// Set a logger on this server to ensure no provider input reaches the log.
	server := New(nil, nil, nil, slog.New(slog.NewTextHandler(&logs, nil)), false, nil, WithOIDC(s))
	for _, mode := range []string{"no state", "no cookie", "wrong cookie", "provider error"} {
		t.Run(mode, func(t *testing.T) {
			begin := httptest.NewRecorder()
			handler.ServeHTTP(begin, httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login", nil))
			u, _ := url.Parse(begin.Header().Get("Location"))
			state := u.Query().Get("state")
			callback := "/api/v1/auth/oidc/callback?state=" + state + "&code=secret-code"
			want := "oidc_invalid_state"
			if mode == "no state" {
				callback = "/api/v1/auth/oidc/callback?code=secret-code"
			}
			if mode == "provider error" {
				callback = "/api/v1/auth/oidc/callback?state=" + state + "&error=secret-error&error_description=secret-detail"
				want = "oidc_failed"
			}
			r := httptest.NewRequest(http.MethodGet, callback, nil)
			if mode != "no cookie" {
				cookie := begin.Result().Cookies()[0]
				if mode == "wrong cookie" {
					cookie.Value = "wrong"
				}
				r.AddCookie(cookie)
			}
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, r)
			if got := response.Header().Get("Location"); got != "/login?oidc_error="+want {
				t.Fatalf("failure redirect = %s", got)
			}
			if !strings.Contains(response.Header().Get("Set-Cookie"), "Max-Age=0") {
				t.Fatal("failed callback did not clear cookie")
			}
			body, _ := io.ReadAll(response.Result().Body)
			if strings.Contains(string(body)+logs.String(), "secret-") {
				t.Fatal("provider input leaked")
			}
		})
	}
}

func TestOIDCLoginRateLimitedByClientIP(t *testing.T) {
	p := oidctest.New(t)
	handler := newTestServer(t, WithOIDC(auth.NewOIDC(p.Config())))
	for i := range 6 {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login", nil)
		r.RemoteAddr = "192.0.2.1:1234"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		if i < 5 && response.Code != http.StatusFound {
			t.Fatalf("attempt %d = %d", i, response.Code)
		}
		if i == 5 && response.Header().Get("Location") != "/login?oidc_error=oidc_rate_limited" {
			t.Fatal("OIDC starts not rate limited")
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login", nil)
	r.RemoteAddr = "192.0.2.2:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, r)
	if response.Code != http.StatusFound {
		t.Fatal("unrelated client IP was rate limited")
	}
}

func TestOIDCSecureCookieConfiguration(t *testing.T) {
	server := New(nil, nil, nil, nil, true, nil)
	response := httptest.NewRecorder()
	server.setSessionCookie(response, "session-token")
	server.setOIDCCookie(response, "browser-binding", false)
	server.setOIDCCookie(response, "", true)
	for _, cookie := range response.Result().Cookies() {
		if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
			t.Fatalf("insecure cookie = %#v", cookie)
		}
	}
}
