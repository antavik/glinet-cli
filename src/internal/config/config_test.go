package config

import (
	"flag"
	"testing"
	"time"
)

func TestRegisterFlags(t *testing.T) {
	t.Setenv("GLINET_URL", "https://router.lan")
	t.Setenv("GLINET_USER", "")

	var cfg Config
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg.RegisterFlags(fs)
	if err := fs.Parse([]string{"-timeout", "3s"}); err != nil {
		t.Fatal(err)
	}
	want := Config{URL: "https://router.lan", User: "root", Timeout: 3 * time.Second, Wait: false}
	if cfg != want {
		t.Fatalf("defaults: got %+v, want %+v", cfg, want)
	}

	if err := fs.Parse([]string{"-url", "http://10.0.0.1", "-user", "admin", "-wait"}); err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "http://10.0.0.1" || cfg.User != "admin" {
		t.Fatalf("flags: got %+v, want url and user from flags", cfg)
	}
	if !cfg.Wait {
		t.Errorf("Wait = %v after -wait, want true", cfg.Wait)
	}
}

func TestAccount(t *testing.T) {
	cfg := Config{URL: "http://192.168.8.1/", User: "root"}
	if got, want := cfg.Account(), "root@http://192.168.8.1"; got != want {
		t.Errorf("Account() = %q, want %q", got, want)
	}
}
