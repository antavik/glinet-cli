package vpn

import (
	"reflect"
	"testing"

	"github.com/antavik/glinet-cli/src/internal/glinet"
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
