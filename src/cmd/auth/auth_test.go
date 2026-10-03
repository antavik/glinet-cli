package auth

import (
	"context"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/antavik/glinet-cli/src/internal/config"
	"github.com/antavik/glinet-cli/src/internal/keychain"
)

func TestLogout(t *testing.T) {
	keyring.MockInit()
	t.Setenv("GLINET_PASSWORD", "")
	cfg := config.Config{URL: "http://192.168.8.1", User: "root"}
	if err := keychain.Save(cfg.Account(), "saved"); err != nil {
		t.Fatal(err)
	}

	if err := logout(context.Background(), cfg); err != nil {
		t.Fatalf("logout() = %v", err)
	}
	if _, err := keychain.Password(cfg.Account()); err == nil {
		t.Fatal("password still saved after logout")
	}
	if err := logout(context.Background(), cfg); err != nil {
		t.Fatalf("logout() without saved password = %v, want nil", err)
	}
}
