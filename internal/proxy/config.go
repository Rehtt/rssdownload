package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Config describes the optional proxy used for outbound RSS and torrent requests.
// Credentials are kept separate from URL so callers can expose the URL without
// accidentally returning the password.
type Config struct {
	URL      string
	Username string
	Password string
}

// Validate checks whether the proxy configuration can be used by net/http.
func (c Config) Validate() error {
	_, err := c.parsedURL()
	return err
}

// NewTransport returns a copy of the standard HTTP transport configured with c.
// An empty proxy URL preserves Go's standard HTTP_PROXY/HTTPS_PROXY behavior.
func NewTransport(c Config) (*http.Transport, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default HTTP transport has an unsupported type")
	}
	cloned := transport.Clone()

	proxyURL, err := c.parsedURL()
	if err != nil {
		return nil, err
	}
	if proxyURL == nil {
		cloned.Proxy = http.ProxyFromEnvironment
	} else {
		cloned.Proxy = http.ProxyURL(proxyURL)
	}
	return cloned, nil
}

func (c Config) parsedURL() (*url.URL, error) {
	rawURL := strings.TrimSpace(c.URL)
	username := strings.TrimSpace(c.Username)
	if rawURL == "" {
		if username != "" || c.Password != "" {
			return nil, errors.New("proxy credentials require a proxy URL")
		}
		return nil, nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, errors.New("proxy URL must use HTTP(S) or SOCKS5")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return nil, errors.New("proxy URL must include a host")
	}
	if parsed.User != nil {
		return nil, errors.New("proxy URL must not contain credentials")
	}
	if (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("proxy URL must not contain a path, query, or fragment")
	}
	if c.Password != "" && username == "" {
		return nil, errors.New("proxy password requires a username")
	}

	parsed.Scheme = scheme
	parsed.Path = ""
	if username != "" {
		if c.Password == "" {
			parsed.User = url.User(username)
		} else {
			parsed.User = url.UserPassword(username, c.Password)
		}
	}
	return parsed, nil
}
