package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/Rehtt/rssdownload/internal/oidctest"
)

func startOIDC(t *testing.T, s *OIDCService) (url.Values, string) {
	t.Helper()
	location, binding, err := s.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(location)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("authorization status: %d", response.StatusCode)
	}
	u, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Query(), binding
}

func TestOIDCIdentityValidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		setup    func(*oidctest.Provider)
		want     error
		userinfo int64
	}{
		{name: "verified email"},
		{name: "nonce", setup: func(p *oidctest.Provider) { p.Claims = func(c map[string]any) { c["nonce"] = "wrong" } }, want: ErrOIDCIdentity},
		{name: "issuer", setup: func(p *oidctest.Provider) {
			p.Claims = func(c map[string]any) { c["iss"] = "https://other.example.com" }
		}, want: ErrOIDCIdentity},
		{name: "audience", setup: func(p *oidctest.Provider) { p.Claims = func(c map[string]any) { c["aud"] = "other-client" } }, want: ErrOIDCIdentity},
		{name: "expired", setup: func(p *oidctest.Provider) {
			p.Claims = func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() }
		}, want: ErrOIDCIdentity},
		{name: "signature", setup: func(p *oidctest.Provider) { p.BadSignature = true }, want: ErrOIDCIdentity},
		{name: "subject missing", setup: func(p *oidctest.Provider) { p.Claims = func(c map[string]any) { delete(c, "sub") } }, want: ErrOIDCIdentity},
		{name: "unverified", setup: func(p *oidctest.Provider) { p.Claims = func(c map[string]any) { c["email_verified"] = false } }, want: ErrOIDCAccess},
		{name: "malformed verification", setup: func(p *oidctest.Provider) { p.Claims = func(c map[string]any) { c["email_verified"] = "true" } }, want: ErrOIDCIdentity},
		{name: "not allowed", setup: func(p *oidctest.Provider) { p.Claims = func(c map[string]any) { c["email"] = "mallory@example.com" } }, want: ErrOIDCAccess},
		{name: "email suffix", setup: func(p *oidctest.Provider) {
			p.Claims = func(c map[string]any) { c["email"] = "alice@example.com.evil" }
		}, want: ErrOIDCAccess},
		{name: "userinfo email", setup: func(p *oidctest.Provider) { p.Claims = func(c map[string]any) { delete(c, "email") } }, userinfo: 1},
		{name: "userinfo verification", setup: func(p *oidctest.Provider) { p.Claims = func(c map[string]any) { delete(c, "email_verified") } }, userinfo: 1},
		{name: "userinfo subject mismatch", setup: func(p *oidctest.Provider) {
			p.Claims = func(c map[string]any) { delete(c, "email") }
			p.UserInfo["sub"] = "different-subject"
		}, want: ErrOIDCIdentity, userinfo: 1},
		{name: "userinfo unverified", setup: func(p *oidctest.Provider) {
			p.Claims = func(c map[string]any) { delete(c, "email") }
			p.UserInfo["email_verified"] = false
		}, want: ErrOIDCAccess, userinfo: 1},
		{name: "userinfo no email", setup: func(p *oidctest.Provider) {
			p.Claims = func(c map[string]any) { delete(c, "email") }
			delete(p.UserInfo, "email")
		}, want: ErrOIDCAccess, userinfo: 1},
		{name: "userinfo no verification", setup: func(p *oidctest.Provider) {
			p.Claims = func(c map[string]any) { delete(c, "email") }
			delete(p.UserInfo, "email_verified")
		}, want: ErrOIDCAccess, userinfo: 1},
		{name: "userinfo malformed verification", setup: func(p *oidctest.Provider) {
			p.Claims = func(c map[string]any) { delete(c, "email") }
			p.UserInfo["email_verified"] = "true"
		}, want: ErrOIDCIdentity, userinfo: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := oidctest.New(t)
			if tc.setup != nil {
				tc.setup(p)
			}
			s := NewOIDC(p.Config())
			q, binding := startOIDC(t, s)
			err := s.Complete(context.Background(), q.Get("state"), binding, q.Get("code"))
			if !errors.Is(err, tc.want) {
				t.Fatalf("Complete = %v, want %v", err, tc.want)
			}
			if got := p.UserInfoCalls.Load(); got != tc.userinfo {
				t.Fatalf("userinfo calls = %d, want %d", got, tc.userinfo)
			}
			if err := s.Complete(context.Background(), q.Get("state"), binding, q.Get("code")); !errors.Is(err, ErrOIDCState) {
				t.Fatalf("replayed callback = %v", err)
			}
		})
	}
}

func TestOIDCStateValidation(t *testing.T) {
	p := oidctest.New(t)
	s := NewOIDC(p.Config())
	for _, mode := range []string{"missing state", "wrong state", "missing cookie", "wrong cookie", "expired", "no code"} {
		t.Run(mode, func(t *testing.T) {
			q, binding := startOIDC(t, s)
			state, code := q.Get("state"), q.Get("code")
			want := ErrOIDCState
			switch mode {
			case "missing state":
				state = ""
			case "wrong state":
				state = "wrong"
			case "missing cookie":
				binding = ""
			case "wrong cookie":
				binding = "wrong"
			case "expired":
				s.mu.Lock()
				tx := s.transactions[state]
				tx.expires = time.Now().Add(-time.Second)
				s.transactions[state] = tx
				s.mu.Unlock()
			case "no code":
				code = ""
				want = ErrOIDCIdentity
			}
			if err := s.Complete(context.Background(), state, binding, code); !errors.Is(err, want) {
				t.Fatalf("Complete = %v, want %v", err, want)
			}
		})
	}
	if calls := p.TokenCalls.Load(); calls != 0 {
		t.Fatalf("invalid states reached token endpoint %d times", calls)
	}
}

func TestOIDCDiscoveryCacheAndRecovery(t *testing.T) {
	p := oidctest.New(t)
	p.FailDiscovery = true
	s := NewOIDC(p.Config())
	if _, _, err := s.Begin(context.Background()); !errors.Is(err, ErrOIDCUnavailable) {
		t.Fatalf("Begin = %v", err)
	}
	p.FailDiscovery = false
	q, binding := startOIDC(t, s)
	if err := s.Complete(context.Background(), q.Get("state"), binding, q.Get("code")); err != nil {
		t.Fatal(err)
	}
	startOIDC(t, s)
	if got := p.DiscoveryCalls.Load(); got != 2 {
		t.Fatalf("discovery requests = %d, want failed attempt + cached success", got)
	}
}

func TestOIDCProviderTimeout(t *testing.T) {
	p := oidctest.New(t)
	p.SlowDiscovery = true
	s := NewOIDC(p.Config())
	s.client.Timeout = 30 * time.Millisecond
	start := time.Now()
	if _, _, err := s.Begin(context.Background()); !errors.Is(err, ErrOIDCUnavailable) {
		t.Fatalf("Begin = %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("provider timeout not respected")
	}
}

func TestOIDCConcurrentStateConsumption(t *testing.T) {
	p := oidctest.New(t)
	s := NewOIDC(p.Config())
	q, binding := startOIDC(t, s)
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- s.Complete(context.Background(), q.Get("state"), binding, q.Get("code")) }()
	}
	a, b := <-results, <-results
	if !(a == nil && errors.Is(b, ErrOIDCState) || b == nil && errors.Is(a, ErrOIDCState)) {
		t.Fatalf("concurrent results: %v, %v", a, b)
	}
}

func TestOIDCPKCERejectsWrongVerifier(t *testing.T) {
	p := oidctest.New(t)
	s := NewOIDC(p.Config())
	q, binding := startOIDC(t, s)
	s.mu.Lock()
	tx := s.transactions[q.Get("state")]
	tx.verifier = "incorrect-verifier-that-does-not-match-the-original-challenge"
	s.transactions[q.Get("state")] = tx
	s.mu.Unlock()
	if err := s.Complete(context.Background(), q.Get("state"), binding, q.Get("code")); !errors.Is(err, ErrOIDCIdentity) {
		t.Fatalf("wrong PKCE verifier = %v", err)
	}
}

func TestOIDCExpiredTransactionCleanup(t *testing.T) {
	p := oidctest.New(t)
	s := NewOIDC(p.Config())
	q, _ := startOIDC(t, s)
	now := time.Now()
	s.now = func() time.Time { return now.Add(OIDCTransactionLifetime) }
	if _, _, err := s.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.transactions[q.Get("state")]; ok {
		t.Fatal("expired transaction retained")
	}
}
