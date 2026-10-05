package glinettest

import (
	"encoding/json"
	"slices"
	"sync"
)

// Tunnel is a VPN client tunnel as the fake router stores it.
type Tunnel struct {
	ID      int    `json:"tunnel_id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Status  int    `json:"status"` // 0 disconnected, 1 connected, 2 connecting
}

// SetCall is one recorded vpn-client.set_tunnel request.
type SetCall struct {
	ID      int
	Enabled bool
}

// VPN is a stateful vpn-client module: set_tunnel changes what get_status
// reports, so a test can turn a tunnel off and then list it. Enabling a
// tunnel marks it connected, disabling it disconnected. It is safe for
// concurrent use.
type VPN struct {
	mu      sync.Mutex
	tunnels []Tunnel
	calls   []SetCall
	fail    func(SetCall) *RPCError
}

// NewVPN returns a vpn-client module holding tunnels. Pass its Handlers to
// NewRouter.
func NewVPN(tunnels ...Tunnel) *VPN {
	return &VPN{tunnels: slices.Clone(tunnels)}
}

// FailWith makes set_tunnel answer with the error fail returns for a call, if
// not nil, and leave the tunnel unchanged. The call is still recorded.
func (v *VPN) FailWith(fail func(SetCall) *RPCError) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.fail = fail
}

// Calls returns every set_tunnel request so far, in order, failed ones too.
func (v *VPN) Calls() []SetCall {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.calls)
}

// Handlers returns the vpn-client.get_status and vpn-client.set_tunnel
// handlers. Merge them with others into the map passed to NewRouter.
func (v *VPN) Handlers() map[string]Handler {
	return map[string]Handler{
		"vpn-client.get_status": func(json.RawMessage) any {
			v.mu.Lock()
			defer v.mu.Unlock()
			return map[string]any{"status_list": slices.Clone(v.tunnels)}
		},
		"vpn-client.set_tunnel": func(args json.RawMessage) any {
			var a struct {
				TunnelID int  `json:"tunnel_id"`
				Enabled  bool `json:"enabled"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return RPCError{Code: -32602, Message: "Invalid params"}
			}
			call := SetCall{a.TunnelID, a.Enabled}

			v.mu.Lock()
			defer v.mu.Unlock()
			v.calls = append(v.calls, call)
			if v.fail != nil {
				if err := v.fail(call); err != nil {
					return err
				}
			}
			i := slices.IndexFunc(v.tunnels, func(t Tunnel) bool { return t.ID == a.TunnelID })
			if i < 0 {
				return RPCError{Code: -32000, Message: "tunnel not found"}
			}
			v.tunnels[i].Enabled = a.Enabled
			v.tunnels[i].Status = 0
			if a.Enabled {
				v.tunnels[i].Status = 1
			}
			return map[string]any{"tunnel_id": a.TunnelID}
		},
	}
}
