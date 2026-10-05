package auth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/antavik/glinet-cli/src/cmd"
	"github.com/antavik/glinet-cli/src/internal/config"
	"github.com/antavik/glinet-cli/src/internal/glinet/glinettest"
	"github.com/antavik/glinet-cli/src/internal/keychain"
)

func TestLogin(t *testing.T) {
	tests := []struct {
		name    string
		stdin   string
		wantErr string // "" for success
	}{
		{"saves a checked password", glinettest.Password + "\n", ""},
		{"accepts CRLF", glinettest.Password + "\r\n", ""},
		{"no trailing newline", glinettest.Password, ""},
		{"wrong password", "wrong\n", "Access denied"},
		{"empty password", "\n", "empty password"},
		{"empty stdin", "", "empty password"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyring.MockInit()
			t.Setenv("GLINET_PASSWORD", "")
			router := glinettest.NewRouter(t, nil)
			cfg := config.Config{URL: router.URL, User: glinettest.User, Timeout: time.Minute}
			var out, errOut bytes.Buffer

			err := login(t.Context(), cfg, cmd.IO{In: strings.NewReader(tt.stdin), Out: &out, Err: &errOut})

			saved, getErr := keychain.Password(cfg.Account())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("login() error = %v, want it to contain %q", err, tt.wantErr)
				}
				// A password the router did not accept is never saved.
				if getErr == nil {
					t.Errorf("keychain holds %q after a failed login", saved)
				}
				return
			}
			if err != nil {
				t.Fatalf("login() error = %v", err)
			}
			if getErr != nil || saved != glinettest.Password {
				t.Errorf("keychain = %q, %v; want %q", saved, getErr, glinettest.Password)
			}
			if !router.LoggedOut() {
				t.Error("login() left the check session open")
			}
			if want := "Logged in. Password saved to the OS keychain.\n"; out.String() != want {
				t.Errorf("stdout = %q, want %q", out.String(), want)
			}
			// Piped input is not a terminal: no prompt.
			if errOut.Len() != 0 {
				t.Errorf("stderr = %q, want nothing for piped input", errOut.String())
			}
		})
	}
}

// Ctrl+C ends a read from a pipe that never sends a line.
func TestReadPasswordCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, w := io.Pipe()
		defer w.Close() // ends the read still waiting on the pipe

		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(time.Second, cancel) // Ctrl+C while waiting

		_, err := readPassword(ctx, cmd.IO{In: r, Err: io.Discard}, "Password: ")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("readPassword() error = %v, want context.Canceled", err)
		}
	})
}

func TestLogout(t *testing.T) {
	keyring.MockInit()
	t.Setenv("GLINET_PASSWORD", "")
	cfg := config.Config{URL: "http://192.168.8.1", User: "root"}
	if err := keychain.Save(cfg.Account(), "saved"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	stdio := cmd.IO{Out: &out}

	if err := logout(t.Context(), cfg, stdio); err != nil {
		t.Fatalf("logout() = %v", err)
	}
	if _, err := keychain.Password(cfg.Account()); err == nil {
		t.Fatal("password still saved after logout")
	}
	if err := logout(t.Context(), cfg, stdio); err != nil {
		t.Fatalf("logout() without saved password = %v, want nil", err)
	}
	want := "Password removed from the OS keychain.\nNo saved password.\n"
	if out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
}
