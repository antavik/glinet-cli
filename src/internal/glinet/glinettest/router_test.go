package glinettest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/antavik/glinet-cli/src/internal/glinet"
)

// callTunnels logs in to a fake router whose vpn-client.get_status handler
// returns result, and makes one Tunnels call against it.
func callTunnels(t *testing.T, result any) ([]glinet.Tunnel, error) {
	t.Helper()
	router := NewRouter(t, map[string]Handler{
		"vpn-client.get_status": func(json.RawMessage) any { return result },
	})
	c := glinet.NewClient(router.URL)
	if err := c.Login(t.Context(), User, Password); err != nil {
		t.Fatal(err)
	}
	return c.Tunnels(t.Context())
}

func TestRPCError(t *testing.T) {
	for _, tt := range []struct {
		name   string
		result any
	}{
		{"value", RPCError{Code: -32000, Message: "boom"}},
		{"pointer", &RPCError{Code: -32000, Message: "boom"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := callTunnels(t, tt.result)
			if err == nil || !strings.Contains(err.Error(), "boom") {
				t.Fatalf("Tunnels() error = %v, want it to contain %q", err, "boom")
			}
		})
	}
}

// A nil *RPCError is an ordinary result, not an error, so handlers can return
// one unconditionally. The null result decodes to no tunnels.
func TestNilRPCErrorPointerIsResult(t *testing.T) {
	tunnels, err := callTunnels(t, (*RPCError)(nil))
	if err != nil {
		t.Fatalf("Tunnels() error = %v, want nil", err)
	}
	if len(tunnels) != 0 {
		t.Errorf("Tunnels() = %+v, want no tunnels", tunnels)
	}
}

func TestSessions(t *testing.T) {
	router := NewRouter(t, map[string]Handler{
		"vpn-client.get_status": func(json.RawMessage) any { return map[string]any{"status_list": []any{}} },
	})
	if router.LoggedOut() {
		t.Fatal("LoggedOut() = true before any login")
	}

	// Two logins in a row, like two CLI runs: logging out the first must not
	// end the second.
	first, second := glinet.NewClient(router.URL), glinet.NewClient(router.URL)
	for _, c := range []*glinet.Client{first, second} {
		if err := c.Login(t.Context(), User, Password); err != nil {
			t.Fatal(err)
		}
	}
	first.Logout()
	if router.LoggedOut() {
		t.Error("LoggedOut() = true with a session still open")
	}
	if _, err := second.Tunnels(t.Context()); err != nil {
		t.Errorf("second session after first logout: %v", err)
	}
	second.Logout()
	if !router.LoggedOut() {
		t.Error("LoggedOut() = false after every session logged out")
	}
}
