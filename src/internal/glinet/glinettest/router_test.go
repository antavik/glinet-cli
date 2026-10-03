package glinettest

import (
	"context"
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
	if err := c.Login(context.Background(), User, Password); err != nil {
		t.Fatal(err)
	}
	return c.Tunnels(context.Background())
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
