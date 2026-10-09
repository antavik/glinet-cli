package status

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/antavik/glinet-cli/src/internal/glinet"
	"github.com/antavik/glinet-cli/src/internal/glinet/glinettest"
)

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "0m"},
		{59 * time.Second, "0m"},
		{90 * time.Minute, "1h 30m"},
		{24*time.Hour + 5*time.Minute, "1d 0h 5m"},
		{3*24*time.Hour + 4*time.Hour + 5*time.Minute + 6*time.Second, "3d 4h 5m"},
	}
	for _, tt := range tests {
		if got := formatUptime(tt.in); got != tt.want {
			t.Errorf("formatUptime(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1 KiB"},
		{1536, "2 KiB"},
		{1048063, "1023 KiB"},
		{1048064, "1 MiB"},
		{1048576, "1 MiB"},
		{1<<30 - 1<<19 - 1, "1023 MiB"},
		{1<<30 - 1<<19, "1.0 GiB"},
		{241172480, "230 MiB"},
		{1073741824, "1.0 GiB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.in); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWanText(t *testing.T) {
	cable := glinet.WanStatus{Protocol: "dhcp", Status: 1, IPv4: glinet.WanIPv4{IP: "10.0.0.2", Gateway: "10.0.0.1", DNS: []string{"1.1.1.1"}}}
	unplugged := glinet.WanStatus{Protocol: "dhcp", Status: 3}
	nets := func(online ...string) []glinet.Network {
		var n []glinet.Network
		for _, i := range []string{"wan", "wwan", "tethering", "wan6"} {
			n = append(n, glinet.Network{Interface: i, Online: slices.Contains(online, i)})
		}
		return n
	}
	noWAN := fmt.Errorf("%w (err_code -5)", glinet.ErrNoWAN)
	tests := []struct {
		name   string
		nets   []glinet.Network
		wan    glinet.WanStatus
		wanErr error
		want   string
	}{
		{"cable online", nets("wan"), cable, nil, "online via cable  dhcp 10.0.0.2  gw 10.0.0.1  dns 1.1.1.1"},
		{"repeater online, cable unplugged", nets("wwan"), unplugged, nil, "online via repeater"},
		{"two uplinks", nets("wan", "tethering"), cable, nil, "online via cable, tethering  dhcp 10.0.0.2  gw 10.0.0.1  dns 1.1.1.1"},
		{"ipv6 only is not an uplink", nets("wan6"), unplugged, nil, "offline  cable no cable"},
		{"cable up, no internet", nets(), cable, nil, "offline  cable connected  dhcp 10.0.0.2  gw 10.0.0.1  dns 1.1.1.1"},
		{"offline, cable failed", nets(), glinet.WanStatus{}, errors.New("boom"), "offline  cable unknown"},
		{"no uplink list, cable only", nil, unplugged, nil, "cable no cable"},
		{"no uplink list, no cable WAN", nil, glinet.WanStatus{}, noWAN, "no cable WAN"},
		{"no uplink list, cable failed", nil, glinet.WanStatus{}, errors.New("boom"), "cable unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := overview{st: glinet.SystemStatus{Networks: tt.nets}, wan: tt.wan, wanErr: tt.wanErr}
			if got := wanText(o); got != tt.want {
				t.Errorf("wanText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMemoryUsage(t *testing.T) {
	st := glinet.SystemStatus{MemoryTotal: 1000, MemoryFree: 300, MemoryBuffCache: 200}
	if got, want := memoryUsage(st), (usage{TotalBytes: 1000, UsedBytes: 500, UsedPercent: 50}); got != want {
		t.Errorf("memoryUsage() = %+v, want %+v", got, want)
	}
	// Bad counters never underflow.
	st = glinet.SystemStatus{MemoryTotal: 1000, MemoryFree: 900, MemoryBuffCache: 200}
	if got, want := memoryUsage(st), (usage{TotalBytes: 1000}); got != want {
		t.Errorf("memoryUsage() = %+v, want %+v", got, want)
	}
}

func TestFirmwareText(t *testing.T) {
	info := glinet.DeviceInfo{FirmwareVersion: "4.9.0"}
	tests := []struct {
		o    overview
		want string
	}{
		{overview{info: info}, "4.9.0 (up to date)"},
		{overview{info: info, upd: glinet.FirmwareUpdate{NewVersion: "4.10.0"}}, "4.9.0 (update 4.10.0 available)"},
		{overview{info: info, updErr: errors.New("boom")}, "4.9.0 (update check failed)"},
	}
	for _, tt := range tests {
		if got := firmwareText(tt.o); got != tt.want {
			t.Errorf("firmwareText() = %q, want %q", got, tt.want)
		}
	}
}

func TestVPNText(t *testing.T) {
	tests := []struct {
		o    overview
		want string
	}{
		{overview{tunErr: errors.New("boom")}, "unknown"},
		{overview{tunnels: []glinet.Tunnel{{ID: 1, Name: "A"}}}, "none enabled"},
		{overview{tunnels: []glinet.Tunnel{{ID: 1, Name: "A", Enabled: true}, {ID: 2, Name: "B"}}}, "A #1 disconnected"},
	}
	for _, tt := range tests {
		if got := vpnText(tt.o); got != tt.want {
			t.Errorf("vpnText() = %q, want %q", got, tt.want)
		}
	}
}

// TestFetchOptional checks that a hung firmware check fails alone: once on
// its own cap, and once on the command's deadline, which WAN and VPN beat
// only because they run alongside it.
func TestFetchOptional(t *testing.T) {
	tests := []struct {
		name            string
		optionalTimeout time.Duration
		deadline        time.Duration // 0 for none
	}{
		{"per-call cap", 100 * time.Millisecond, 0},
		{"parallel", time.Hour, 300 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := optionalTimeout
			optionalTimeout = tt.optionalTimeout
			t.Cleanup(func() { optionalTimeout = old })

			release := make(chan struct{})
			handlers := glinettest.NewVPN(glinettest.Tunnel{ID: 1, Name: "A", Enabled: true, Status: 1}).Handlers()
			handlers["system.get_info"] = func(json.RawMessage) any { return map[string]any{"firmware_version": "4.9.0"} }
			handlers["system.get_status"] = func(json.RawMessage) any { return map[string]any{} }
			handlers["cable.get_status"] = func(json.RawMessage) any { return map[string]any{"protocol": "dhcp", "status": 1} }
			handlers["upgrade.check_firmware_online"] = func(json.RawMessage) any {
				<-release
				return map[string]any{}
			}
			router := glinettest.NewRouter(t, handlers)
			// Cleanups run last-in first-out: free the hung handler before the
			// router's server waits for it to return.
			t.Cleanup(func() { close(release) })
			c := glinet.NewClient(router.URL)
			if err := c.Login(t.Context(), glinettest.User, glinettest.Password); err != nil {
				t.Fatal(err)
			}

			ctx := t.Context()
			if tt.deadline > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.deadline)
				defer cancel()
			}
			o, err := fetch(ctx, c)
			if err != nil {
				t.Fatal(err)
			}
			if !errors.Is(o.updErr, context.DeadlineExceeded) {
				t.Errorf("firmware error = %v, want deadline exceeded", o.updErr)
			}
			if o.wanErr != nil || o.tunErr != nil {
				t.Errorf("WAN error = %v, VPN error = %v; want both nil", o.wanErr, o.tunErr)
			}
		})
	}
}
