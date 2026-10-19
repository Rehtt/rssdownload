package config

import (
	"errors"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

const (
	defaultListen  = "127.0.0.1:8080"
	defaultDataDir = "./data"
)

type Config struct {
	Listen         string
	DataDir        string
	CookieSecure   bool
	AdminUsername  string
	AdminPassword  string
	TrustedProxies []netip.Prefix
	OIDC           OIDCConfig
}

func Load(args []string) (Config, error) {
	cfg := Config{
		Listen:        envOr("RSSDOWNLOAD_LISTEN", defaultListen),
		DataDir:       envOr("RSSDOWNLOAD_DATA_DIR", defaultDataDir),
		CookieSecure:  envBool("RSSDOWNLOAD_COOKIE_SECURE", false),
		AdminUsername: envOr("RSSDOWNLOAD_ADMIN_USERNAME", "admin"),
		AdminPassword: os.Getenv("RSSDOWNLOAD_ADMIN_PASSWORD"),
		OIDC: OIDCConfig{
			IssuerURL:    os.Getenv("RSSDOWNLOAD_OIDC_ISSUER_URL"),
			ClientID:     os.Getenv("RSSDOWNLOAD_OIDC_CLIENT_ID"),
			ClientSecret: os.Getenv("RSSDOWNLOAD_OIDC_CLIENT_SECRET"),
			RedirectURL:  os.Getenv("RSSDOWNLOAD_OIDC_REDIRECT_URL"),
		},
	}
	trustedProxies := os.Getenv("RSSDOWNLOAD_TRUSTED_PROXIES")

	fs := flag.NewFlagSet("rssdownload", flag.ContinueOnError)
	fs.StringVar(&cfg.Listen, "listen", cfg.Listen, "HTTP listen address")
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "directory for SQLite data")
	fs.BoolVar(&cfg.CookieSecure, "cookie-secure", cfg.CookieSecure, "set Secure on the session cookie")
	fs.StringVar(
		&trustedProxies,
		"trusted-proxies",
		trustedProxies,
		"comma-separated proxy IPs or CIDRs whose X-Forwarded-* headers are trusted",
	)
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() != 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if cfg.Listen == "" {
		return Config{}, errors.New("listen address must not be empty")
	}
	if cfg.DataDir == "" {
		return Config{}, errors.New("data directory must not be empty")
	}
	if cfg.AdminUsername == "" {
		return Config{}, errors.New("admin username must not be empty")
	}
	parsed, err := parseTrustedProxies(trustedProxies)
	if err != nil {
		return Config{}, err
	}
	cfg.TrustedProxies = parsed
	if err := cfg.OIDC.validate(os.Getenv("RSSDOWNLOAD_OIDC_ALLOWED_EMAILS")); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func parseTrustedProxies(value string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			prefixes = append(prefixes, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy %q: expected an IP or CIDR", entry)
		}
		prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return prefixes, nil
}

func envOr(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value, ok := os.LookupEnv(name)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
