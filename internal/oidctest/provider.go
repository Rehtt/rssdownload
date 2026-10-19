// Package oidctest provides a local identity provider for authentication tests.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Rehtt/rssdownload/internal/config"
	"github.com/go-jose/go-jose/v4"
)

var signingKey = sync.OnceValue(func() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key
})

type authorization struct{ nonce, challenge, redirect string }

type Provider struct {
	Server *httptest.Server
	// Set these before requests; mutation during requests is unsupported.
	Claims         func(map[string]any)
	UserInfo       map[string]any
	BadSignature   bool
	FailDiscovery  bool
	SlowDiscovery  bool
	DiscoveryCalls atomic.Int64
	TokenCalls     atomic.Int64
	UserInfoCalls  atomic.Int64
	mu             sync.Mutex
	codes          map[string]authorization
}

func New(t testing.TB) *Provider {
	t.Helper()
	p := &Provider{
		codes:    make(map[string]authorization),
		UserInfo: map[string]any{"sub": "subject-123", "email": "alice@example.com", "email_verified": true},
	}
	p.Server = httptest.NewServer(http.HandlerFunc(p.serveHTTP))
	t.Cleanup(p.Server.Close)
	return p
}

func (p *Provider) Config() config.OIDCConfig {
	return config.OIDCConfig{
		IssuerURL: p.Server.URL, ClientID: "rssdownload", ClientSecret: "client-secret",
		RedirectURL:   "http://localhost/api/v1/auth/oidc/callback",
		AllowedEmails: []string{"alice@example.com"},
	}
}

func (p *Provider) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		p.DiscoveryCalls.Add(1)
		if p.SlowDiscovery {
			<-r.Context().Done()
			return
		}
		if p.FailDiscovery {
			http.Error(w, "unavailable secret-provider-error", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                p.Server.URL,
			"authorization_endpoint":                p.Server.URL + "/authorize",
			"token_endpoint":                        p.Server.URL + "/token",
			"userinfo_endpoint":                     p.Server.URL + "/userinfo",
			"jwks_uri":                              p.Server.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	case "/jwks":
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &signingKey().PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}})
	case "/authorize":
		q := r.URL.Query()
		if q.Get("client_id") != "rssdownload" || q.Get("response_type") != "code" || q.Get("scope") != "openid email" ||
			q.Get("nonce") == "" || q.Get("state") == "" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
			http.Error(w, "invalid authorization request", http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		code := fmt.Sprintf("code-%d", len(p.codes)+1)
		p.codes[code] = authorization{q.Get("nonce"), q.Get("code_challenge"), q.Get("redirect_uri")}
		p.mu.Unlock()
		u, err := url.Parse(q.Get("redirect_uri"))
		if err != nil {
			http.Error(w, "invalid redirect", http.StatusBadRequest)
			return
		}
		values := u.Query()
		values.Set("state", q.Get("state"))
		values.Set("code", code)
		u.RawQuery = values.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
	case "/token":
		p.TokenCalls.Add(1)
		_ = r.ParseForm()
		p.mu.Lock()
		a, ok := p.codes[r.Form.Get("code")]
		delete(p.codes, r.Form.Get("code"))
		p.mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		clientID, secret, basic := r.BasicAuth()
		if !basic {
			clientID, secret = r.Form.Get("client_id"), r.Form.Get("client_secret")
		}
		if !ok || r.Form.Get("grant_type") != "authorization_code" || clientID != "rssdownload" || secret != "client-secret" ||
			r.Form.Get("redirect_uri") != a.redirect || len(r.Form.Get("code_verifier")) < 43 ||
			base64.RawURLEncoding.EncodeToString(sum[:]) != a.challenge {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"secret-provider-error"}`))
			return
		}
		claims := map[string]any{
			"iss": p.Server.URL, "sub": "subject-123", "aud": "rssdownload",
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": a.nonce,
			"email": "Alice@Example.COM", "email_verified": true,
		}
		if p.Claims != nil {
			p.Claims(claims)
		}
		key := signingKey()
		if p.BadSignature {
			key, _ = rsa.GenerateKey(rand.Reader, 2048)
		}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "test"))
		if err != nil {
			http.Error(w, "signer failed", http.StatusInternalServerError)
			return
		}
		payload, _ := json.Marshal(claims)
		signed, _ := signer.Sign(payload)
		raw, _ := signed.CompactSerialize()
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access-token", "token_type": "Bearer", "expires_in": 3600, "id_token": raw})
	case "/userinfo":
		p.UserInfoCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer access-token" {
			http.Error(w, "invalid bearer token", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(p.UserInfo)
	default:
		http.NotFound(w, r)
	}
}
