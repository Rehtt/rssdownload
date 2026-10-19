package config

import (
	"errors"
	"fmt"
	"net/mail"
	"net/netip"
	"net/url"
	"strings"
)

type OIDCConfig struct {
	IssuerURL     string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	AllowedEmails []string
}

func (c OIDCConfig) Enabled() bool { return c.IssuerURL != "" }

func (c *OIDCConfig) validate(emails string) error {
	c.IssuerURL = strings.TrimSpace(c.IssuerURL)
	c.ClientID = strings.TrimSpace(c.ClientID)
	c.RedirectURL = strings.TrimSpace(c.RedirectURL)
	if c.IssuerURL == "" && c.ClientID == "" && c.ClientSecret == "" && c.RedirectURL == "" && emails == "" {
		return nil
	}
	if c.IssuerURL == "" || c.ClientID == "" || strings.TrimSpace(c.ClientSecret) == "" || c.RedirectURL == "" || strings.TrimSpace(emails) == "" {
		return errors.New("all RSSDOWNLOAD_OIDC_* settings, including a non-empty email allowlist, are required")
	}
	for _, entry := range strings.Split(emails, ",") {
		email := strings.ToLower(strings.TrimSpace(entry))
		if email == "" {
			continue
		}
		parsed, err := mail.ParseAddress(email)
		if err != nil || parsed.Address != email || strings.ContainsAny(email, "*<>") {
			return errors.New("RSSDOWNLOAD_OIDC_ALLOWED_EMAILS must contain complete email addresses without wildcards or display names")
		}
		c.AllowedEmails = append(c.AllowedEmails, email)
	}
	if len(c.AllowedEmails) == 0 {
		return errors.New("RSSDOWNLOAD_OIDC_ALLOWED_EMAILS must not be empty")
	}
	for name, raw := range map[string]string{"ISSUER_URL": c.IssuerURL, "REDIRECT_URL": c.RedirectURL} {
		if err := ValidateOIDCURL(raw); err != nil {
			return fmt.Errorf("RSSDOWNLOAD_OIDC_%s: %w", name, err)
		}
		u, _ := url.Parse(raw)
		if u.RawQuery != "" || u.ForceQuery {
			return fmt.Errorf("RSSDOWNLOAD_OIDC_%s must not contain a query", name)
		}
		if name == "REDIRECT_URL" && u.Path != "/api/v1/auth/oidc/callback" {
			return errors.New("RSSDOWNLOAD_OIDC_REDIRECT_URL must use /api/v1/auth/oidc/callback")
		}
	}
	return nil
}

// ValidateOIDCURL permits cleartext HTTP only for local development.
func ValidateOIDCURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return errors.New("expected an absolute HTTPS URL without credentials or fragment")
	}
	if u.Scheme == "https" {
		return nil
	}
	addr, _ := netip.ParseAddr(u.Hostname())
	if u.Scheme == "http" && (strings.EqualFold(u.Hostname(), "localhost") || addr.IsValid() && addr.Unmap().IsLoopback()) {
		return nil
	}
	return errors.New("HTTPS is required (HTTP is allowed only on localhost or loopback IPs)")
}
