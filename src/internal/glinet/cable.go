package glinet

import (
	"context"
	"errors"
	"fmt"
)

// WanIPv4 is the IPv4 setup of the cable WAN.
type WanIPv4 struct {
	IP      string
	Gateway string
	DNS     []string
}

// WanStatus is the state of the cable WAN port.
type WanStatus struct {
	Protocol string // e.g. "dhcp" or "static"
	Status   int    // 0 disconnected, 1 connected, 2 connecting, 3 no cable
	IPv4     WanIPv4
}

// ErrNoWAN means the router reports no cable WAN (a negative err_code).
var ErrNoWAN = errors.New("no usable WAN")

// WanStatus returns the cable WAN state, with text made safe to print. The
// error wraps ErrNoWAN when the router has no cable WAN.
func (c *Client) WanStatus(ctx context.Context) (WanStatus, error) {
	var res struct {
		Protocol string `json:"protocol"`
		Status   int    `json:"status"`
		IPv4     struct {
			IP      string   `json:"ip"`
			Gateway string   `json:"gateway"`
			DNS     []string `json:"dns"`
		} `json:"ipv4"`
		ErrCode int `json:"err_code"`
	}
	if err := c.call(ctx, "cable", "get_status", nil, &res); err != nil {
		return WanStatus{}, err
	}
	if res.ErrCode < 0 {
		return WanStatus{}, fmt.Errorf("%w (err_code %d)", ErrNoWAN, res.ErrCode)
	}
	if res.Protocol == "" {
		return WanStatus{}, errors.New("WAN status has no protocol")
	}
	for i := range res.IPv4.DNS {
		res.IPv4.DNS[i] = printable(res.IPv4.DNS[i])
	}
	return WanStatus{
		Protocol: printable(res.Protocol),
		Status:   res.Status,
		IPv4: WanIPv4{
			IP:      printable(res.IPv4.IP),
			Gateway: printable(res.IPv4.Gateway),
			DNS:     res.IPv4.DNS,
		},
	}, nil
}
