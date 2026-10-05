// Package vpn implements "glinet-cli vpn": listing VPN client tunnels and
// turning them on or off.
package vpn

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/antavik/glinet-cli/src/cmd"
	"github.com/antavik/glinet-cli/src/internal/glinet"
)

func init() {
	cmd.Register(cmd.Command{
		Name: "vpn",
		Usage: []string{
			"vpn [status]\tlist VPN tunnels",
			"vpn on <id|name> | -all [-wait]\tturn VPN tunnel(s) on (-wait: until connected)",
			"vpn off <id|name> | -all\tturn VPN tunnel(s) off",
			"vpn restart <id|name> | -all [-wait]\trestart VPN tunnel(s), off then on (-wait: until connected)",
		},
		Parse: parse,
	})
}

func parse(fs *flag.FlagSet, args []string) (cmd.Action, error) {
	all := fs.Bool("all", false, "target every tunnel")
	wait := fs.Bool("wait", false, "after on or restart, wait until the tunnel is connected")
	a, err := cmd.ParseArgs(fs, args)
	if err != nil {
		return nil, err
	}
	switch a.Sub {
	case "", "status":
		if len(a.Pos) != 0 || *all || *wait {
			return nil, errors.New("vpn status takes no arguments")
		}
		return cmd.WithClient(printTunnels), nil
	case "on", "off", "restart":
		target, err := setTarget(a.Pos, *all)
		if err != nil {
			return nil, err
		}
		if *wait && a.Sub == "off" {
			return nil, errors.New("-wait works with on and restart only")
		}
		if a.Sub == "restart" {
			return cmd.WithClient(func(ctx context.Context, c *glinet.Client, stdio cmd.IO) error {
				return restartTunnels(ctx, c, stdio.Out, target, *all, *wait)
			}), nil
		}
		enable := a.Sub == "on"
		return cmd.WithClient(func(ctx context.Context, c *glinet.Client, stdio cmd.IO) error {
			return setTunnels(ctx, c, stdio.Out, target, *all, enable, *wait)
		}), nil
	}
	return nil, fmt.Errorf("unknown vpn subcommand %q", a.Sub)
}

// setTarget returns the one tunnel ID or name, or "" with -all.
func setTarget(pos []string, all bool) (string, error) {
	switch {
	case all && len(pos) == 0:
		return "", nil
	case !all && len(pos) == 1:
		return pos[0], nil
	}
	return "", errors.New("expected one tunnel ID or name, or -all")
}

func printTunnels(ctx context.Context, c *glinet.Client, stdio cmd.IO) error {
	tunnels, err := c.Tunnels(ctx)
	if err != nil {
		return err
	}
	if len(tunnels) == 0 {
		fmt.Fprintln(stdio.Out, "No VPN tunnels configured.")
		return nil
	}

	w := tabwriter.NewWriter(stdio.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tENABLED\tSTATUS")
	for _, t := range tunnels {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", t.ID, t.Name, onOff(t.Enabled), statusText(t))
	}
	return w.Flush()
}

// setTunnels turns matching tunnels on or off, skipping those already there.
// With wait, it waits for each enabled tunnel to connect. It reports all
// errors and stops once ctx ends.
func setTunnels(ctx context.Context, c *glinet.Client, w io.Writer, target string, all, enable, wait bool) error {
	tunnels, err := c.Tunnels(ctx)
	if err != nil {
		return err
	}
	selected, err := selectTunnels(tunnels, target, all)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		fmt.Fprintln(w, "No VPN tunnels configured.")
		return nil
	}

	var errs []error
	for _, t := range selected {
		if t.Enabled == enable {
			fmt.Fprintf(w, "%s: already %s\n", t.Name, onOff(enable))
		} else {
			// Report cancellation once, not per remaining tunnel.
			if err := ctx.Err(); err != nil {
				errs = append(errs, err)
				break
			}
			if err := c.SetTunnel(ctx, t.ID, enable); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", t.Name, err))
				continue
			}
			fmt.Fprintf(w, "%s: %s\n", t.Name, onOff(enable))
		}
		if wait && enable {
			if err := waitConnected(ctx, c, w, t); err != nil {
				errs = append(errs, err)
				if ctx.Err() != nil {
					break // ctx ended: skip remaining waits
				}
			}
		}
	}
	return errors.Join(errs...)
}

// restartTunnels turns matching tunnels off, then on, one at a time, whatever
// their state. A tunnel whose off fails is not turned on. With wait, it waits
// for each to connect. It reports all errors and stops once ctx ends.
func restartTunnels(ctx context.Context, c *glinet.Client, w io.Writer, target string, all, wait bool) error {
	tunnels, err := c.Tunnels(ctx)
	if err != nil {
		return err
	}
	selected, err := selectTunnels(tunnels, target, all)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		fmt.Fprintln(w, "No VPN tunnels configured.")
		return nil
	}

	var errs []error
	for _, t := range selected {
		// Ctrl+C or the command timeout stops the loop instead of letting
		// every remaining tunnel fail one by one.
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if err := c.SetTunnel(ctx, t.ID, false); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", t.Name, err))
			continue
		}
		if err := c.SetTunnel(ctx, t.ID, true); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", t.Name, err))
			continue
		}
		fmt.Fprintf(w, "%s: restarted\n", t.Name)
		if wait {
			if err := waitConnected(ctx, c, w, t); err != nil {
				errs = append(errs, err)
				if ctx.Err() != nil {
					break // ctx ended: skip remaining waits
				}
			}
		}
	}
	return errors.Join(errs...)
}

// pollInterval is the delay between waitConnected status checks.
var pollInterval = time.Second

// statusConnected is the get_status code for connected (0 not started,
// 2 connecting).
const statusConnected = 1

// waitConnected polls get_status until t is connected or ctx ends.
func waitConnected(ctx context.Context, c *glinet.Client, w io.Writer, t glinet.Tunnel) error {
	for {
		tunnels, err := c.Tunnels(ctx)
		if err != nil {
			return fmt.Errorf("%s: %w", t.Name, err)
		}
		i := slices.IndexFunc(tunnels, func(u glinet.Tunnel) bool { return u.ID == t.ID })
		if i < 0 {
			return fmt.Errorf("%s: tunnel disappeared", t.Name)
		}
		if tunnels[i].Status == statusConnected {
			fmt.Fprintf(w, "%s: connected\n", t.Name)
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: not connected: %w", t.Name, ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

// selectTunnels returns every tunnel with all, else the one whose ID or name
// (case-insensitive) is target. An ID match wins over a name match.
func selectTunnels(tunnels []glinet.Tunnel, target string, all bool) ([]glinet.Tunnel, error) {
	if all {
		return tunnels, nil
	}
	if i := slices.IndexFunc(tunnels, func(t glinet.Tunnel) bool { return strconv.Itoa(t.ID) == target }); i >= 0 {
		return []glinet.Tunnel{tunnels[i]}, nil
	}
	var matched []glinet.Tunnel
	for _, t := range tunnels {
		if strings.EqualFold(t.Name, target) {
			matched = append(matched, t)
		}
	}
	switch len(matched) {
	case 0:
		return nil, fmt.Errorf("no VPN tunnel matches %q", target)
	case 1:
		return matched, nil
	default:
		return nil, fmt.Errorf("%q matches %d VPN tunnels, use the tunnel ID", target, len(matched))
	}
}

// statusText describes a tunnel's connection state. The router's status code
// only means something while the tunnel is enabled.
func statusText(t glinet.Tunnel) string {
	if !t.Enabled {
		return "-"
	}
	switch t.Status {
	case 0:
		return "disconnected"
	case statusConnected:
		return "connected"
	case 2:
		return "connecting"
	default:
		return fmt.Sprintf("unknown (%d)", t.Status)
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
