package glinet

import (
	"context"
	"fmt"
)

// WanIPv4 is the WAN interface's IPv4 configuration.
type WanIPv4 struct {
	IP      string
	Gateway string
	DNS     []string
}

// WanStatus is the state of the WAN (cable) interface.
type WanStatus struct {
	Protocol string
	Status   int
	IPv4     WanIPv4
}

// WanStatus returns the WAN interface's protocol, up/down state and IPv4
// settings, with strings made safe to print. The router reports a negative
// err_code as a field of the result payload when it has no usable WAN: -4
// means no physical WAN port and -5 means no virtual WAN is configured.
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
		return WanStatus{}, fmt.Errorf("no usable WAN (err_code %d)", res.ErrCode)
	}
	if res.Protocol == "" {
		return WanStatus{}, fmt.Errorf("WAN status has no protocol")
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
