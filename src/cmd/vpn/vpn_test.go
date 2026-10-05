package vpn

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/antavik/glinet-cli/src/cmd"
	"github.com/antavik/glinet-cli/src/internal/config"
	"github.com/antavik/glinet-cli/src/internal/glinet"
	"github.com/antavik/glinet-cli/src/internal/glinet/glinettest"
)

func TestSelectTunnels(t *testing.T) {
	home := glinet.Tunnel{ID: 2001, Name: "Home/WG"}
	work := glinet.Tunnel{ID: 2002, Name: "Work/OVPN"}
	dup := glinet.Tunnel{ID: 2003, Name: "home/wg"}
	numeric := glinet.Tunnel{ID: 2004, Name: "2001"}
	all := glinet.Tunnel{ID: 2005, Name: "all"}

	tests := []struct {
		name    string
		tunnels []glinet.Tunnel
		target  string
		all     bool
		want    []glinet.Tunnel
		wantErr bool
	}{
		{"all", []glinet.Tunnel{home, work}, "", true, []glinet.Tunnel{home, work}, false},
		{"by id", []glinet.Tunnel{home, work}, "2002", false, []glinet.Tunnel{work}, false},
		{"by name ignoring case", []glinet.Tunnel{home, work}, "work/ovpn", false, []glinet.Tunnel{work}, false},
		{"no match", []glinet.Tunnel{home, work}, "nope", false, nil, true},
		{"ambiguous name", []glinet.Tunnel{home, dup}, "Home/WG", false, nil, true},
		{"id of duplicate name", []glinet.Tunnel{home, dup}, "2003", false, []glinet.Tunnel{dup}, false},
		{"id wins over numeric name", []glinet.Tunnel{home, numeric}, "2001", false, []glinet.Tunnel{home}, false},
		{"numeric name", []glinet.Tunnel{work, numeric}, "2001", false, []glinet.Tunnel{numeric}, false},
		{"tunnel named all, by name", []glinet.Tunnel{home, all}, "all", false, []glinet.Tunnel{all}, false},
		{"target all, no such tunnel", []glinet.Tunnel{home, work}, "all", false, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectTunnels(tt.tunnels, tt.target, tt.all)
			if (err != nil) != tt.wantErr {
				t.Fatalf("selectTunnels() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
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

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool // true for a non-nil action
	}{
		{"bare", nil, true},
		{"status", []string{"status"}, true},
		{"on -all", []string{"on", "-all"}, true},
		{"off -all", []string{"off", "-all"}, true},
		{"restart -all", []string{"restart", "-all"}, true},
		{"on by id", []string{"on", "2001"}, true},
		{"off by name", []string{"off", "Home/WG"}, true},
		{"restart by id", []string{"restart", "2001"}, true},
		{"on -all=false with target", []string{"on", "-all=false", "2001"}, true},
		{"on after -- terminator", []string{"on", "--", "-corp"}, true},
		{"on missing target", []string{"on"}, false},
		{"on -all with target", []string{"on", "-all", "2001"}, false},
		{"on two positionals", []string{"on", "a", "b"}, false},
		{"on unknown flag", []string{"on", "-bogus"}, false},
		{"restart missing target", []string{"restart"}, false},
		{"restart -all with target", []string{"restart", "-all", "x"}, false},
		{"toggle all", []string{"toggle", "all"}, false},
		{"off -all=false without target", []string{"off", "-all=false"}, false},
		{"on -wait", []string{"on", "2001", "-wait"}, true},
		{"restart -all -wait", []string{"restart", "-wait", "-all"}, true},
		{"off -wait", []string{"off", "2001", "-wait"}, false},
		{"status -wait", []string{"status", "-wait"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			action, err := parse(fs, tt.args)
			if got := action != nil && err == nil; got != tt.want {
				t.Errorf("parse(%q) = %v, %v; want ok %v", tt.args, action != nil, err, tt.want)
			}
		})
	}
}

// Default tunnels for the tests below: one on and connected, two off.
var (
	home   = glinettest.Tunnel{ID: 2001, Name: "Home/WG", Enabled: true, Status: 1}
	work   = glinettest.Tunnel{ID: 2002, Name: "Work/OVPN"}
	travel = glinettest.Tunnel{ID: 2003, Name: "Travel/WG"}
)

// fixture returns a client logged in to a fake router holding tunnels.
func fixture(t *testing.T, tunnels ...glinettest.Tunnel) (*glinet.Client, *glinettest.VPN) {
	t.Helper()
	vpn := glinettest.NewVPN(tunnels...)
	c := glinet.NewClient(glinettest.NewRouter(t, vpn.Handlers()).URL)
	if err := c.Login(t.Context(), glinettest.User, glinettest.Password); err != nil {
		t.Fatal(err)
	}
	return c, vpn
}

// on and off are the set_tunnel calls that turn tunnel id on or off.
func on(id int) glinettest.SetCall  { return glinettest.SetCall{ID: id, Enabled: true} }
func off(id int) glinettest.SetCall { return glinettest.SetCall{ID: id, Enabled: false} }

// failOn makes set_tunnel fail for exactly the given call.
func failOn(call glinettest.SetCall) func(glinettest.SetCall) *glinettest.RPCError {
	return func(c glinettest.SetCall) *glinettest.RPCError {
		if c == call {
			return &glinettest.RPCError{Code: -1, Message: "injected failure"}
		}
		return nil
	}
}

func TestSetTunnels(t *testing.T) {
	tests := []struct {
		name      string
		tunnels   []glinettest.Tunnel
		target    string
		all       bool
		enable    bool
		fail      *glinettest.SetCall
		wantCalls []glinettest.SetCall
		wantOut   string
		wantErr   []string // substrings; nil for success
	}{
		{
			name:    "on all skips tunnels already on",
			tunnels: []glinettest.Tunnel{home, work, travel}, all: true, enable: true,
			wantCalls: []glinettest.SetCall{on(2002), on(2003)},
			wantOut:   "Home/WG: already on\nWork/OVPN: on\nTravel/WG: on\n",
		},
		{
			name:    "off by id",
			tunnels: []glinettest.Tunnel{home, work}, target: "2001", enable: false,
			wantCalls: []glinettest.SetCall{off(2001)},
			wantOut:   "Home/WG: off\n",
		},
		{
			name:    "a failure does not stop the others",
			tunnels: []glinettest.Tunnel{home, work, travel}, all: true, enable: true,
			fail:      new(on(2002)),
			wantCalls: []glinettest.SetCall{on(2002), on(2003)},
			wantOut:   "Home/WG: already on\nTravel/WG: on\n",
			wantErr:   []string{"Work/OVPN", "injected failure"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, vpn := fixture(t, tt.tunnels...)
			if tt.fail != nil {
				vpn.FailWith(failOn(*tt.fail))
			}
			var out bytes.Buffer

			err := setTunnels(t.Context(), c, &out, tt.target, tt.all, tt.enable, false)

			checkErr(t, err, tt.wantErr)
			if got := vpn.Calls(); !slices.Equal(got, tt.wantCalls) {
				t.Errorf("set_tunnel calls = %+v, want %+v", got, tt.wantCalls)
			}
			if out.String() != tt.wantOut {
				t.Errorf("output = %q, want %q", out.String(), tt.wantOut)
			}
		})
	}
}

func TestRestartTunnels(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		all       bool
		fail      *glinettest.SetCall
		wantCalls []glinettest.SetCall
		wantOut   string
		wantErr   []string
	}{
		{
			// Restart ignores the current state: the disabled tunnel ends up on.
			name: "-all, one tunnel at a time",
			all:  true,
			wantCalls: []glinettest.SetCall{
				off(2001), on(2001),
				off(2002), on(2002),
			},
			wantOut: "Home/WG: restarted\nWork/OVPN: restarted\n",
		},
		{
			// A failed off must not be followed by an on for that tunnel.
			name: "off fails",
			all:  true,
			fail: new(off(2001)),
			wantCalls: []glinettest.SetCall{
				off(2001),
				off(2002), on(2002),
			},
			wantOut: "Work/OVPN: restarted\n",
			wantErr: []string{"Home/WG", "injected failure"},
		},
		{
			// A failed on is reported; the next tunnel is still processed.
			name: "on fails",
			all:  true,
			fail: new(on(2001)),
			wantCalls: []glinettest.SetCall{
				off(2001), on(2001),
				off(2002), on(2002),
			},
			wantOut: "Work/OVPN: restarted\n",
			wantErr: []string{"Home/WG", "injected failure"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, vpn := fixture(t, home, work)
			if tt.fail != nil {
				vpn.FailWith(failOn(*tt.fail))
			}
			var out bytes.Buffer

			err := restartTunnels(t.Context(), c, &out, tt.target, tt.all, false)

			checkErr(t, err, tt.wantErr)
			if got := vpn.Calls(); !slices.Equal(got, tt.wantCalls) {
				t.Errorf("set_tunnel calls = %+v, want %+v", got, tt.wantCalls)
			}
			if out.String() != tt.wantOut {
				t.Errorf("output = %q, want %q", out.String(), tt.wantOut)
			}
		})
	}
}

// Ctrl+C during -all stops before the next tunnel and reports it once.
func TestStopsWhenCancelled(t *testing.T) {
	tests := []struct {
		name      string
		run       func(context.Context, *glinet.Client) error
		wantCalls []glinettest.SetCall
	}{
		{
			name: "on",
			run: func(ctx context.Context, c *glinet.Client) error {
				return setTunnels(ctx, c, &bytes.Buffer{}, "", true, true, false)
			},
			wantCalls: []glinettest.SetCall{on(2002)},
		},
		{
			name: "restart",
			run: func(ctx context.Context, c *glinet.Client) error {
				return restartTunnels(ctx, c, &bytes.Buffer{}, "", true, false)
			},
			wantCalls: []glinettest.SetCall{off(2001)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, vpn := fixture(t, home, work, travel)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			vpn.FailWith(func(glinettest.SetCall) *glinettest.RPCError {
				cancel() // Ctrl+C while the first set_tunnel is in flight
				return nil
			})

			err := tt.run(ctx, c)

			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
			if strings.Contains(err.Error(), "Travel/WG") {
				t.Errorf("error = %q, want no failure for tunnels after the cancel", err)
			}
			if got := vpn.Calls(); !slices.Equal(got, tt.wantCalls) {
				t.Errorf("set_tunnel calls = %+v, want %+v", got, tt.wantCalls)
			}
		})
	}
}

// -wait polls until each tunnel is connected, already enabled ones included.
func TestWaitConnected(t *testing.T) {
	pollInterval = time.Millisecond
	tests := []struct {
		name    string
		run     func(context.Context, *glinet.Client, io.Writer) error
		wantOut string
	}{
		{
			name: "on -all",
			run: func(ctx context.Context, c *glinet.Client, w io.Writer) error {
				return setTunnels(ctx, c, w, "", true, true, true)
			},
			wantOut: "Home/WG: already on\nHome/WG: connected\n" +
				"Work/OVPN: on\nWork/OVPN: connected\n",
		},
		{
			name: "restart",
			run: func(ctx context.Context, c *glinet.Client, w io.Writer) error {
				return restartTunnels(ctx, c, w, "2002", false, true)
			},
			wantOut: "Work/OVPN: restarted\nWork/OVPN: connected\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, vpn := fixture(t, home, work)
			vpn.ConnectAfter(3)
			var out bytes.Buffer

			if err := tt.run(t.Context(), c, &out); err != nil {
				t.Fatalf("error = %v, output %q", err, out.String())
			}
			if out.String() != tt.wantOut {
				t.Errorf("output = %q, want %q", out.String(), tt.wantOut)
			}
		})
	}
}

// The timeout fails the wait and skips the remaining tunnels.
func TestWaitTimeout(t *testing.T) {
	pollInterval = time.Millisecond
	c, vpn := fixture(t, work, travel)
	vpn.ConnectAfter(1 << 30)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	err := setTunnels(ctx, c, io.Discard, "", true, true, true)

	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "Work/OVPN: not connected") {
		t.Fatalf("error = %v, want Work/OVPN not connected: deadline exceeded", err)
	}
	if strings.Contains(err.Error(), "Travel/WG") {
		t.Errorf("error = %q, want no failure for tunnels after the timeout", err)
	}
	if want := []glinettest.SetCall{on(2002)}; !slices.Equal(vpn.Calls(), want) {
		t.Errorf("set_tunnel calls = %+v, want %+v", vpn.Calls(), want)
	}
}

// "on -all" works through the real login path, password from GLINET_PASSWORD.
func TestParseOnAllAction(t *testing.T) {
	t.Setenv("GLINET_PASSWORD", glinettest.Password)
	vpn := glinettest.NewVPN(home, work, travel)
	cfg := config.Config{URL: glinettest.NewRouter(t, vpn.Handlers()).URL, User: glinettest.User, Timeout: time.Minute}

	action, err := parse(flag.NewFlagSet("", flag.ContinueOnError), []string{"on", "-all"})
	if action == nil || err != nil {
		t.Fatalf("parse([on -all]) = %v, %v; want action", action != nil, err)
	}
	var out bytes.Buffer
	if err := action(t.Context(), cfg, cmd.IO{Out: &out}); err != nil {
		t.Fatalf("action() error = %v, output %q", err, out.String())
	}
	if want := []glinettest.SetCall{on(2002), on(2003)}; !slices.Equal(vpn.Calls(), want) {
		t.Errorf("set_tunnel calls = %+v, want %+v", vpn.Calls(), want)
	}
	if want := "Home/WG: already on\nWork/OVPN: on\nTravel/WG: on\n"; out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
}

// checkErr fails t unless err contains every substring in want, or is nil
// when want is nil.
func checkErr(t *testing.T, err error, want []string) {
	t.Helper()
	if want == nil {
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("error = nil, want one containing %q", want)
	}
	for _, s := range want {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), s)
		}
	}
}
