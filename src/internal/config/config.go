// Package config holds the global flags every command gets.
package config

import (
	"flag"
	"os"
	"strings"
	"time"
)

// Config holds the global flags.
type Config struct {
	URL     string
	User    string
	Timeout time.Duration
	Wait    bool
}

// RegisterFlags defines the global flags on fs. Defaults come from the
// environment where a variable exists.
func (cfg *Config) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(&cfg.URL, "url", envOr("GLINET_URL", "http://192.168.8.1"), "router URL (env GLINET_URL)")
	fs.StringVar(&cfg.User, "user", envOr("GLINET_USER", "root"), "router username (env GLINET_USER)")
	fs.DurationVar(&cfg.Timeout, "timeout", 30*time.Second, "timeout for router requests")
	fs.BoolVar(&cfg.Wait, "wait", false, "wait for the router to finish async operations")
}

// Account names the router and user as "user@url". It keys the saved password.
func (cfg Config) Account() string {
	return cfg.User + "@" + strings.TrimRight(cfg.URL, "/")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
