package glinet

import (
	"context"
	"time"
)

// Uptime returns how long the router has been running.
func (c *Client) Uptime(ctx context.Context) (time.Duration, error) {
	var res struct {
		System struct {
			Uptime float64 `json:"uptime"` // seconds
		} `json:"system"`
	}
	if err := c.call(ctx, "system", "get_status", nil, &res); err != nil {
		return 0, err
	}
	return time.Duration(res.System.Uptime * float64(time.Second)), nil
}
