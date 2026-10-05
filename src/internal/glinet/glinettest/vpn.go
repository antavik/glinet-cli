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
// reports. Enabled tunnels connect at once unless ConnectAfter is set. Safe
// for concurrent use.
type VPN struct {
	mu           sync.Mutex
	tunnels      []Tunnel
	calls        []SetCall
	fail         func(SetCall) *RPCError
	connectPolls int
	connecting   map[int]int // tunnel ID -> get_status calls left before connected
}

// NewVPN returns a vpn-client module holding tunnels.
func NewVPN(tunnels ...Tunnel) *VPN {
	return &VPN{tunnels: slices.Clone(tunnels)}
}

// FailWith makes set_tunnel return fail's non-nil error, leaving the tunnel
// unchanged. The call is still recorded.
func (v *VPN) FailWith(fail func(SetCall) *RPCError) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.fail = fail
}

// ConnectAfter makes newly enabled tunnels report connecting for n
// get_status calls.
func (v *VPN) ConnectAfter(n int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.connectPolls = n
}

// Calls returns every set_tunnel request so far, failed ones too.
func (v *VPN) Calls() []SetCall {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.calls)
}

// Handlers returns the get_status and set_tunnel handlers for NewRouter.
func (v *VPN) Handlers() map[string]Handler {
	return map[string]Handler{
		"vpn-client.get_status": func(json.RawMessage) any {
			v.mu.Lock()
			defer v.mu.Unlock()
			list := slices.Clone(v.tunnels)
			for i, t := range v.tunnels {
				if n, ok := v.connecting[t.ID]; ok && t.Status == 2 {
					if v.connecting[t.ID] = n - 1; n <= 1 {
						v.tunnels[i].Status = 1
					}
				}
			}
			return map[string]any{"status_list": list}
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
			switch {
			case a.Enabled && v.connectPolls > 0:
				v.tunnels[i].Status = 2
				if v.connecting == nil {
					v.connecting = map[int]int{}
				}
				v.connecting[a.TunnelID] = v.connectPolls
			case a.Enabled:
				v.tunnels[i].Status = 1
			}
			return map[string]any{"tunnel_id": a.TunnelID}
		},
	}
}
