// Package vpn implements "glinet-cli vpn": listing VPN client tunnels and
// turning them on or off.
package vpn

import (
	"context"
	"errors"
	"fmt"
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
			"vpn on <id|name|all>\tturn VPN tunnel(s) on",
			"vpn off <id|name|all>\tturn VPN tunnel(s) off",
			"vpn restart <id|name|all>\trestart VPN tunnel(s) (off, then on)",
		},
		Parse: parse,
	})
}

func parse(args []string) cmd.Action {
	switch {
	case len(args) == 0, len(args) == 1 && args[0] == "status":
		return cmd.WithClient(printTunnels)
	case len(args) == 2 && (args[0] == "on" || args[0] == "off"):
		target, enable := args[1], args[0] == "on"
		return cmd.WithClient(func(ctx context.Context, c *glinet.Client) error {
			return setTunnels(ctx, c, target, enable)
		})
	case len(args) == 2 && args[0] == "restart":
		target := args[1]
		return cmd.WithClient(func(ctx context.Context, c *glinet.Client) error {
			return restartTunnels(ctx, c, target)
		})
	}
	return nil
}

func printTunnels(ctx context.Context, c *glinet.Client) error {
	tunnels, err := c.Tunnels(ctx)
	if err != nil {
		return err
	}
	if len(tunnels) == 0 {
		fmt.Println("No VPN tunnels configured.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tENABLED\tSTATUS")
	for _, t := range tunnels {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", t.ID, t.Name, onOff(t.Enabled), statusText(t))
	}
	return w.Flush()
}

// setTunnels turns the tunnels matching target on or off, skipping those
// already in that state. It keeps going after a failure and reports all errors.
func setTunnels(ctx context.Context, c *glinet.Client, target string, enable bool) error {
	tunnels, err := c.Tunnels(ctx)
	if err != nil {
		return err
	}
	selected, err := selectTunnels(tunnels, target)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		fmt.Println("No VPN tunnels configured.")
		return nil
	}

	var errs []error
	for _, t := range selected {
		if t.Enabled == enable {
			fmt.Printf("%s: already %s\n", t.Name, onOff(enable))
			continue
		}
		if err := c.SetTunnel(ctx, t.ID, enable); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", t.Name, err))
			continue
		}
		fmt.Printf("%s: %s\n", t.Name, onOff(enable))
	}
	return errors.Join(errs...)
}

// restartTunnels turns each tunnel matching target off, then on, one tunnel at
// a time, ignoring the current state, so a disabled tunnel ends up enabled. A
// tunnel whose off fails is not turned on. It keeps going after a failure and
// reports all errors, and stops early once ctx is cancelled.
func restartTunnels(ctx context.Context, c *glinet.Client, target string) error {
	tunnels, err := c.Tunnels(ctx)
	if err != nil {
		return err
	}
	selected, err := selectTunnels(tunnels, target)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		fmt.Println("No VPN tunnels configured.")
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
		fmt.Printf("%s: restarted\n", t.Name)
	}
	return errors.Join(errs...)
}

// selectTunnels returns all tunnels for "all", otherwise the one tunnel whose
// ID or name (case-insensitive) equals target. IDs are unique, so an ID match
// wins over a tunnel named like another tunnel's ID.
func selectTunnels(tunnels []glinet.Tunnel, target string) ([]glinet.Tunnel, error) {
	if target == "all" {
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
