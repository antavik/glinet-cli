// Package vpn implements "glinet-cli vpn": listing VPN client tunnels and
// turning them on or off.
package vpn

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/antavik/glinet-cli/src/cmd"
	"github.com/antavik/glinet-cli/src/internal/glinet"
)

func init() {
	cmd.Register(cmd.Command{
		Name: "vpn",
		Usage: []string{
			"vpn [status]\tlist VPN tunnels",
			"vpn on <id|name> | -all\tturn VPN tunnel(s) on",
			"vpn off <id|name> | -all\tturn VPN tunnel(s) off",
			"vpn restart <id|name> | -all\trestart VPN tunnel(s) (off, then on)",
		},
		Parse: parse,
	})
}

func parse(args []string) cmd.Action {
	switch {
	case len(args) == 0, len(args) == 1 && args[0] == "status":
		return cmd.WithClient(printTunnels)
	case len(args) >= 1 && (args[0] == "on" || args[0] == "off"):
		target, all, ok := setTarget(args[1:])
		if !ok {
			return nil
		}
		enable := args[0] == "on"
		return cmd.WithClient(func(ctx context.Context, c *glinet.Client, stdio cmd.IO) error {
			return setTunnels(ctx, c, stdio.Out, target, all, enable)
		})
	case len(args) >= 1 && args[0] == "restart":
		target, all, ok := setTarget(args[1:])
		if !ok {
			return nil
		}
		return cmd.WithClient(func(ctx context.Context, c *glinet.Client, stdio cmd.IO) error {
			return restartTunnels(ctx, c, stdio.Out, target, all)
		})
	}
	return nil
}

// setTarget parses the target of "on", "off" or "restart": one positional
// tunnel ID or name, or -all for every tunnel. It returns ok=false for
// anything else and prints the reason to stderr, prefixed like the errors
// main prints, so a usage error names its cause above the help text.
func setTarget(args []string) (target string, all bool, ok bool) {
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	allFlag := fs.Bool("all", false, "target every tunnel")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "glinet-cli:", err)
		return "", false, false
	}
	target, all, n := fs.Arg(0), *allFlag, fs.NArg()
	switch {
	case all && n == 0:
		return "", true, true
	case !all && n == 1:
		return target, false, true
	}
	fmt.Fprintln(os.Stderr, "glinet-cli: expected one tunnel ID or name, or -all")
	return "", false, false
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

// setTunnels turns the tunnels matching target on or off, or every tunnel
// when all is set, skipping those already in that state. It keeps going after
// a failure and reports all errors, and stops early once ctx is cancelled.
func setTunnels(ctx context.Context, c *glinet.Client, w io.Writer, target string, all, enable bool) error {
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
			continue
		}
		// As in restartTunnels: after Ctrl+C or the timeout, report the
		// cancellation once instead of one failure per remaining tunnel.
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
	return errors.Join(errs...)
}

// restartTunnels turns each tunnel matching target off, then on, one tunnel at
// a time, or every tunnel when all is set, ignoring the current state, so a
// disabled tunnel ends up enabled. A tunnel whose off fails is not turned on.
// It keeps going after a failure and reports all errors, and stops early once
// ctx is cancelled.
func restartTunnels(ctx context.Context, c *glinet.Client, w io.Writer, target string, all bool) error {
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
	}
	return errors.Join(errs...)
}

// selectTunnels returns every tunnel when all is set, otherwise the one
// tunnel whose ID or name (case-insensitive) equals target. IDs are unique, so
// an ID match wins over a tunnel named like another tunnel's ID. A tunnel
// named "all" matches by name like any other.
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
	case 1:
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
