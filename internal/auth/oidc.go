package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Rehtt/rssdownload/internal/config"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const OIDCTransactionLifetime = 10 * time.Minute

var (
	ErrOIDCState       = errors.New("invalid OIDC transaction")
	ErrOIDCUnavailable = errors.New("OIDC provider unavailable")
	ErrOIDCIdentity    = errors.New("invalid OIDC identity")
	ErrOIDCAccess      = errors.New("OIDC email not allowed or verified")
)

type oidcTransaction struct {
	binding, nonce, verifier string
	expires                  time.Time
}

type OIDCService struct {
	cfg          config.OIDCConfig
	client       *http.Client
	now          func() time.Time
	mu           sync.Mutex
	transactions map[string]oidcTransaction
	discovery    chan struct{}
	provider     *oidc.Provider
	verifier     *oidc.IDTokenVerifier
}

func NewOIDC(cfg config.OIDCConfig) *OIDCService {
	return &OIDCService{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(r *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("too many redirects")
				}
				return config.ValidateOIDCURL(r.URL.String())
			},
		},
		now:          time.Now,
		transactions: make(map[string]oidcTransaction),
		discovery:    make(chan struct{}, 1),
	}
}

func (s *OIDCService) Enabled() bool { return s != nil && s.cfg.Enabled() }

// discover caches only successful discovery. No provider request occurs at startup.
func (s *OIDCService) discover(ctx context.Context) (*oidc.Provider, *oidc.IDTokenVerifier, error) {
	select {
	case s.discovery <- struct{}{}:
		defer func() { <-s.discovery }()
	case <-ctx.Done():
		return nil, nil, ErrOIDCUnavailable
	}
	if s.provider != nil {
		return s.provider, s.verifier, nil
	}
	p, err := oidc.NewProvider(ctx, s.cfg.IssuerURL)
	if err != nil {
		return nil, nil, ErrOIDCUnavailable
	}
	var metadata struct {
		JWKSURL string `json:"jwks_uri"`
	}
	if p.Claims(&metadata) != nil {
		return nil, nil, ErrOIDCUnavailable
	}
	for _, endpoint := range []string{p.Endpoint().AuthURL, p.Endpoint().TokenURL, metadata.JWKSURL} {
		if config.ValidateOIDCURL(endpoint) != nil {
			return nil, nil, ErrOIDCUnavailable
		}
	}
	if endpoint := p.UserInfoEndpoint(); endpoint != "" && config.ValidateOIDCURL(endpoint) != nil {
		return nil, nil, ErrOIDCUnavailable
	}
	s.provider = p
	// Keep the cached JWKS client independent of the first login's request lifetime.
	s.verifier = p.VerifierContext(oidc.ClientContext(context.Background(), s.client), &oidc.Config{ClientID: s.cfg.ClientID})
	return s.provider, s.verifier, nil
}

func (s *OIDCService) oauthConfig(p *oidc.Provider) oauth2.Config {
	return oauth2.Config{
		ClientID: s.cfg.ClientID, ClientSecret: s.cfg.ClientSecret,
		RedirectURL: s.cfg.RedirectURL, Endpoint: p.Endpoint(),
		Scopes: []string{oidc.ScopeOpenID, "email"},
	}
}

func (s *OIDCService) Begin(ctx context.Context) (location, binding string, err error) {
	ctx, cancel := context.WithTimeout(oidc.ClientContext(ctx, s.client), 10*time.Second)
	defer cancel()
	p, _, err := s.discover(ctx)
	if err != nil {
		return "", "", err
	}
	state, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	binding, err = randomToken(32)
	if err != nil {
		return "", "", err
	}
	nonce, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	verifier := oauth2.GenerateVerifier()
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, tx := range s.transactions {
		if !now.Before(tx.expires) {
			delete(s.transactions, key)
		}
	}
	// Bound memory even if clients keep abandoning authorization flows.
	if len(s.transactions) >= 4096 {
		return "", "", ErrOIDCUnavailable
	}
	s.transactions[state] = oidcTransaction{binding: binding, nonce: nonce, verifier: verifier, expires: now.Add(OIDCTransactionLifetime)}
	c := s.oauthConfig(p)
	return c.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), binding, nil
}

// Complete consumes state before making network requests, including failed callbacks.
func (s *OIDCService) Complete(ctx context.Context, state, binding, code string) error {
	s.mu.Lock()
	tx, ok := s.transactions[state]
	delete(s.transactions, state)
	s.mu.Unlock()
	if !ok || !s.now().Before(tx.expires) || binding == "" || subtle.ConstantTimeCompare([]byte(tx.binding), []byte(binding)) != 1 {
		return ErrOIDCState
	}
	if code == "" {
		return ErrOIDCIdentity
	}
	ctx, cancel := context.WithTimeout(oidc.ClientContext(ctx, s.client), 10*time.Second)
	defer cancel()
	p, verifier, err := s.discover(ctx)
	if err != nil {
		return err
	}
	c := s.oauthConfig(p)
	token, err := c.Exchange(ctx, code, oauth2.VerifierOption(tx.verifier))
	if err != nil {
		return ErrOIDCIdentity
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return ErrOIDCIdentity
	}
	id, err := verifier.Verify(ctx, raw)
	if err != nil || id.Subject == "" || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(tx.nonce)) != 1 {
		return ErrOIDCIdentity
	}
	type emailClaims struct {
		Email         string `json:"email"`
		EmailVerified *bool  `json:"email_verified"`
	}
	var claims emailClaims
	if id.Claims(&claims) != nil {
		return ErrOIDCIdentity
	}
	if claims.Email == "" || claims.EmailVerified == nil {
		info, err := p.UserInfo(ctx, oauth2.StaticTokenSource(token))
		if err != nil || info.Subject != id.Subject {
			return ErrOIDCIdentity
		}
		// Use a coherent pair of email claims from the same response.
		claims = emailClaims{}
		if info.Claims(&claims) != nil {
			return ErrOIDCIdentity
		}
	}
	if claims.EmailVerified == nil || !*claims.EmailVerified || claims.Email == "" {
		return ErrOIDCAccess
	}
	for _, allowed := range s.cfg.AllowedEmails {
		if strings.EqualFold(claims.Email, allowed) {
			return nil
		}
	}
	return ErrOIDCAccess
}
