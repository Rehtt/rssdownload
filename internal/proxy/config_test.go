package proxy

import (
	"net/http"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	valid := []Config{
		{},
		{URL: "http://127.0.0.1:7890"},
		{URL: "https://proxy.example:8443/"},
		{URL: "socks5://localhost:1080"},
		{URL: "socks5h://[::1]:1080", Username: "proxy-user", Password: "proxy-pass"},
	}
	for _, config := range valid {
		if err := config.Validate(); err != nil {
			t.Errorf("expected %#v to be valid: %v", config, err)
		}
	}

	invalid := []Config{
		{Username: "proxy-user"},
		{URL: "ftp://proxy.example:21"},
		{URL: "http:///missing-host"},
		{URL: "http://user:pass@proxy.example:7890"},
		{URL: "http://proxy.example:7890/path"},
		{URL: "http://proxy.example:7890?mode=fast"},
		{URL: "http://proxy.example:7890", Password: "proxy-pass"},
	}
	for _, config := range invalid {
		if err := config.Validate(); err == nil {
			t.Errorf("expected %#v to be rejected", config)
		}
	}
}

func TestNewTransportUsesConfiguredProxyAndCredentials(t *testing.T) {
	t.Parallel()

	transport, err := NewTransport(Config{
		URL:      "socks5://proxy.example:1080",
		Username: "proxy-user",
		Password: "proxy-pass",
	})
	if err != nil {
		t.Fatalf("create transport: %v", err)
	}
	request, err := http.NewRequest(http.MethodGet, "https://mikanani.me/", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	proxyURL, err := transport.Proxy(request)
	if err != nil {
		t.Fatalf("resolve proxy: %v", err)
	}
	if proxyURL == nil ||
		proxyURL.Scheme != "socks5" ||
		proxyURL.Host != "proxy.example:1080" ||
		proxyURL.User.Username() != "proxy-user" {
		t.Fatalf("unexpected proxy URL: %#v", proxyURL)
	}
	password, present := proxyURL.User.Password()
	if !present || password != "proxy-pass" {
		t.Fatal("proxy password was not attached to the transport")
	}
}
