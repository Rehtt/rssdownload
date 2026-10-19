package config

import (
	"reflect"
	"testing"
)

var oidcEnv = map[string]string{
	"RSSDOWNLOAD_OIDC_ISSUER_URL":     "https://id.example.com",
	"RSSDOWNLOAD_OIDC_CLIENT_ID":      "rssdownload",
	"RSSDOWNLOAD_OIDC_CLIENT_SECRET":  "secret",
	"RSSDOWNLOAD_OIDC_REDIRECT_URL":   "https://rss.example.com/api/v1/auth/oidc/callback",
	"RSSDOWNLOAD_OIDC_ALLOWED_EMAILS": " Alice@Example.com, BOB@example.com ,",
}

func setOIDCEnv(t *testing.T, enabled bool) {
	t.Helper()
	for key, value := range oidcEnv {
		if !enabled {
			value = ""
		}
		t.Setenv(key, value)
	}
}

func TestOIDCConfiguration(t *testing.T) {
	setOIDCEnv(t, false)
	cfg, err := Load(nil)
	if err != nil || cfg.OIDC.Enabled() {
		t.Fatalf("disabled config = %#v, %v", cfg.OIDC, err)
	}
	setOIDCEnv(t, true)
	cfg, err = Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.OIDC.Enabled() || !reflect.DeepEqual(cfg.OIDC.AllowedEmails, []string{"alice@example.com", "bob@example.com"}) {
		t.Fatalf("OIDC config = %#v", cfg.OIDC)
	}
}

func TestOIDCPartialConfiguration(t *testing.T) {
	for key := range oidcEnv {
		t.Run(key+" missing", func(t *testing.T) {
			setOIDCEnv(t, true)
			t.Setenv(key, "")
			if _, err := Load(nil); err == nil {
				t.Fatal("accepted missing setting")
			}
		})
		t.Run(key+" alone", func(t *testing.T) {
			setOIDCEnv(t, false)
			t.Setenv(key, oidcEnv[key])
			if _, err := Load(nil); err == nil {
				t.Fatal("accepted partial setting")
			}
		})
	}
}

func TestOIDCInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"ALLOWED_EMAILS", " , , "},
		{"ALLOWED_EMAILS", "example.com"},
		{"ALLOWED_EMAILS", "*@example.com"},
		{"ALLOWED_EMAILS", "Alice <alice@example.com>"},
		{"ISSUER_URL", "http://id.example.com"},
		{"ISSUER_URL", "https://id.example.com?query=value"},
		{"ISSUER_URL", "https://user:password@id.example.com"},
		{"REDIRECT_URL", "http://rss.example.com/api/v1/auth/oidc/callback"},
		{"REDIRECT_URL", "https://rss.example.com/wrong-path"},
		{"REDIRECT_URL", "https://rss.example.com/api/v1/auth/oidc/callback#fragment"},
	} {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			setOIDCEnv(t, true)
			t.Setenv("RSSDOWNLOAD_OIDC_"+tc.key, tc.value)
			if _, err := Load(nil); err == nil {
				t.Fatal("accepted invalid setting")
			}
		})
	}
}

func TestOIDCLocalDevelopmentURLs(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "[::1]"} {
		t.Run(host, func(t *testing.T) {
			setOIDCEnv(t, true)
			t.Setenv("RSSDOWNLOAD_OIDC_ISSUER_URL", "http://"+host+":9000/issuer")
			t.Setenv("RSSDOWNLOAD_OIDC_REDIRECT_URL", "http://"+host+":5173/api/v1/auth/oidc/callback")
			if _, err := Load(nil); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, raw := range []string{"http://localhost.evil.com", "http://192.168.1.2", "//localhost", "ftp://localhost"} {
		if err := ValidateOIDCURL(raw); err == nil {
			t.Fatalf("accepted unsafe URL %q", raw)
		}
	}
}
