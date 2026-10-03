package glinet

import "context"

// Tunnel is a VPN client tunnel from the VPN Dashboard (firmware 4.8+).
type Tunnel struct {
	ID      int    `json:"tunnel_id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Status  int    `json:"status"`
}

// Tunnels lists all VPN client tunnels, with names made safe to print.
func (c *Client) Tunnels(ctx context.Context) ([]Tunnel, error) {
	var res struct {
		StatusList []Tunnel `json:"status_list"`
	}
	if err := c.call(ctx, "vpn-client", "get_status", nil, &res); err != nil {
		return nil, err
	}
	for i := range res.StatusList {
		res.StatusList[i].Name = printable(res.StatusList[i].Name)
	}
	return res.StatusList, nil
}

// SetTunnel turns the tunnel with the given ID on or off.
func (c *Client) SetTunnel(ctx context.Context, id int, enabled bool) error {
	return c.call(ctx, "vpn-client", "set_tunnel", map[string]any{"tunnel_id": id, "enabled": enabled}, nil)
}
