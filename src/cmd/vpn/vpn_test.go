package vpn

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/antavik/glinet-cli/src/internal/glinet"
	"github.com/antavik/glinet-cli/src/internal/glinet/glinettest"
)

func TestSelectTunnels(t *testing.T) {
	home := glinet.Tunnel{ID: 2001, Name: "Home/WG"}
	work := glinet.Tunnel{ID: 2002, Name: "Work/OVPN"}
	dup := glinet.Tunnel{ID: 2003, Name: "home/wg"}
	numeric := glinet.Tunnel{ID: 2004, Name: "2001"}

	tests := []struct {
		name    string
		tunnels []glinet.Tunnel
		target  string
		want    []glinet.Tunnel
		wantErr bool
	}{
		{"all", []glinet.Tunnel{home, work}, "all", []glinet.Tunnel{home, work}, false},
		{"by id", []glinet.Tunnel{home, work}, "2002", []glinet.Tunnel{work}, false},
		{"by name ignoring case", []glinet.Tunnel{home, work}, "work/ovpn", []glinet.Tunnel{work}, false},
		{"no match", []glinet.Tunnel{home, work}, "nope", nil, true},
		{"ambiguous name", []glinet.Tunnel{home, dup}, "Home/WG", nil, true},
		{"id of duplicate name", []glinet.Tunnel{home, dup}, "2003", []glinet.Tunnel{dup}, false},
		{"id wins over numeric name", []glinet.Tunnel{home, numeric}, "2001", []glinet.Tunnel{home}, false},
		{"numeric name", []glinet.Tunnel{work, numeric}, "2001", []glinet.Tunnel{numeric}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectTunnels(tt.tunnels, tt.target)
			if (err != nil) != tt.wantErr {
				t.Fatalf("selectTunnels() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("selectTunnels() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestStatusText(t *testing.T) {
	tests := []struct {
		tunnel glinet.Tunnel
		want   string
	}{
		{glinet.Tunnel{Enabled: false, Status: 1}, "-"},
		{glinet.Tunnel{Enabled: true, Status: 0}, "disconnected"},
		{glinet.Tunnel{Enabled: true, Status: 1}, "connected"},
		{glinet.Tunnel{Enabled: true, Status: 2}, "connecting"},
		{glinet.Tunnel{Enabled: true, Status: 7}, "unknown (7)"},
	}
	for _, tt := range tests {
		if got := statusText(tt.tunnel); got != tt.want {
			t.Errorf("statusText(%+v) = %q, want %q", tt.tunnel, got, tt.want)
		}
	}
}

// setCall is one recorded vpn-client.set_tunnel request.
type setCall struct {
	ID      int
	Enabled bool
}

// restartFixture serves two tunnels (2001 enabled, 2002 disabled) and records
// every set_tunnel call in order, including failed ones. fail, if non-nil, may
// return a *glinettest.RPCError to inject a JSON-RPC error for a call.
func restartFixture(t *testing.T, fail func(setCall) *glinettest.RPCError) (*glinet.Client, func() []setCall) {
	t.Helper()

	var (
		mu    sync.Mutex
		calls []setCall
	)
	router := glinettest.NewRouter(t, map[string]glinettest.Handler{
		"vpn-client.get_status": func(json.RawMessage) any {
			return json.RawMessage(`{"status_list":[
				{"tunnel_id":2001,"name":"Home/WG","enabled":true,"status":1},
				{"tunnel_id":2002,"name":"Work/OVPN","enabled":false,"status":0}
			]}`)
		},
		"vpn-client.set_tunnel": func(args json.RawMessage) any {
			var a struct {
				TunnelID int  `json:"tunnel_id"`
				Enabled  bool `json:"enabled"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				t.Errorf("decode set_tunnel args %s: %v", args, err)
			}
			call := setCall{a.TunnelID, a.Enabled}

			mu.Lock()
			calls = append(calls, call)
			mu.Unlock()

			if fail != nil {
				if res := fail(call); res != nil {
					return res
				}
			}
			return map[string]any{"tunnel_id": a.TunnelID}
		},
	})

	c := glinet.NewClient(router.URL)
	if err := c.Login(context.Background(), glinettest.User, glinettest.Password); err != nil {
		t.Fatal(err)
	}
	recorded := func() []setCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]setCall(nil), calls...)
	}
	return c, recorded
}

func TestRestartTunnelsCyclesSequentially(t *testing.T) {
	c, recorded := restartFixture(t, nil)

	if err := restartTunnels(context.Background(), c, "all"); err != nil {
		t.Fatalf("restartTunnels() error = %v", err)
	}

	// The disabled tunnel (2002) is cycled too: restart ignores current state.
	want := []setCall{{2001, false}, {2001, true}, {2002, false}, {2002, true}}
	if got := recorded(); !reflect.DeepEqual(got, want) {
		t.Errorf("set_tunnel calls = %+v, want %+v", got, want)
	}
}

func TestRestartTunnelsByName(t *testing.T) {
	c, recorded := restartFixture(t, nil)

	if err := restartTunnels(context.Background(), c, "work/ovpn"); err != nil {
		t.Fatalf("restartTunnels() error = %v", err)
	}

	want := []setCall{{2002, false}, {2002, true}}
	if got := recorded(); !reflect.DeepEqual(got, want) {
		t.Errorf("set_tunnel calls = %+v, want %+v", got, want)
	}
}

func TestRestartTunnelsUnknownTarget(t *testing.T) {
	c, recorded := restartFixture(t, nil)

	err := restartTunnels(context.Background(), c, "nope")
	if err == nil || !strings.Contains(err.Error(), "no VPN tunnel matches") {
		t.Fatalf("restartTunnels() error = %v, want it to contain %q", err, "no VPN tunnel matches")
	}
	if got := recorded(); len(got) != 0 {
		t.Errorf("set_tunnel calls = %+v, want none", got)
	}
}

func TestRestartTunnelsFailure(t *testing.T) {
	tests := []struct {
		name     string
		failOn   setCall
		wantCall []setCall
	}{
		{
			// A failed off must not be followed by an on for that tunnel.
			name:   "off fails",
			failOn: setCall{2001, false},
			wantCall: []setCall{
				{2001, false},
				{2002, false}, {2002, true},
			},
		},
		{
			// A failed on is reported; the next tunnel is still processed.
			name:   "on fails",
			failOn: setCall{2001, true},
			wantCall: []setCall{
				{2001, false}, {2001, true},
				{2002, false}, {2002, true},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorded := restartFixture(t, func(call setCall) *glinettest.RPCError {
				if call == tt.failOn {
					return &glinettest.RPCError{Code: -1, Message: "injected failure"}
				}
				return nil
			})

			err := restartTunnels(context.Background(), c, "all")
			if err == nil {
				t.Fatal("restartTunnels() error = nil, want error")
			}
			if !strings.Contains(err.Error(), "Home/WG") {
				t.Errorf("restartTunnels() error = %q, want it to name tunnel %q", err, "Home/WG")
			}
			if got := recorded(); !reflect.DeepEqual(got, tt.wantCall) {
				t.Errorf("set_tunnel calls = %+v, want %+v", got, tt.wantCall)
			}
		})
	}
}
