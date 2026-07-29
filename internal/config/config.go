package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
)

const (
	defaultListen  = "127.0.0.1:8080"
	defaultDataDir = "./data"
)

type Config struct {
	Listen        string
	DataDir       string
	CookieSecure  bool
	AdminUsername string
	AdminPassword string
}

func Load(args []string) (Config, error) {
	cfg := Config{
		Listen:        envOr("RSSDOWNLOAD_LISTEN", defaultListen),
		DataDir:       envOr("RSSDOWNLOAD_DATA_DIR", defaultDataDir),
		CookieSecure:  envBool("RSSDOWNLOAD_COOKIE_SECURE", false),
		AdminUsername: envOr("RSSDOWNLOAD_ADMIN_USERNAME", "admin"),
		AdminPassword: os.Getenv("RSSDOWNLOAD_ADMIN_PASSWORD"),
	}

	fs := flag.NewFlagSet("rssdownload", flag.ContinueOnError)
	fs.StringVar(&cfg.Listen, "listen", cfg.Listen, "HTTP listen address")
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "directory for SQLite data")
	fs.BoolVar(&cfg.CookieSecure, "cookie-secure", cfg.CookieSecure, "set Secure on the session cookie")
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
	return cfg, nil
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
