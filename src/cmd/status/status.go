// Package status implements "glinet-cli status": router uptime.
package status

import (
	"context"
	"fmt"
	"time"

	"github.com/antavik/glinet-cli/src/cmd"
	"github.com/antavik/glinet-cli/src/internal/glinet"
)

func init() {
	cmd.Register(cmd.Command{
		Name:  "status",
		Usage: []string{"status\tshow router uptime"},
		Parse: parse,
	})
}

func parse(args []string) cmd.Action {
	if len(args) != 0 {
		return nil
	}
	return cmd.WithClient(printUptime)
}

func printUptime(ctx context.Context, c *glinet.Client, stdio cmd.IO) error {
	uptime, err := c.Uptime(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdio.Out, "Uptime:", formatUptime(uptime))
	return nil
}

// formatUptime renders d as days, hours and minutes, e.g. "3d 4h 5m".
func formatUptime(d time.Duration) string {
	day := 24 * time.Hour
	return fmt.Sprintf("%dd %dh %dm", d/day, d%day/time.Hour, d%time.Hour/time.Minute)
}
