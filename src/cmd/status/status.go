// Package status implements "glinet-cli status": router uptime.
package status

import (
	"context"
	"errors"
	"flag"
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

func parse(fs *flag.FlagSet, args []string) (cmd.Action, error) {
	a, err := cmd.ParseArgs(fs, args)
	if err != nil {
		return nil, err
	}
	if a.Sub != "" {
		return nil, errors.New("status takes no arguments")
	}
	return cmd.WithClient(printUptime), nil
}

func printUptime(ctx context.Context, c *glinet.Client, stdio cmd.IO) error {
	st, err := c.Status(ctx)
	if err != nil {
		return err
	}
	uptime := st.Uptime
	fmt.Fprintln(stdio.Out, "Uptime:", formatUptime(uptime))
	return nil
}

// formatUptime renders d as days, hours and minutes, e.g. "3d 4h 5m".
func formatUptime(d time.Duration) string {
	day := 24 * time.Hour
	return fmt.Sprintf("%dd %dh %dm", d/day, d%day/time.Hour, d%time.Hour/time.Minute)
}
