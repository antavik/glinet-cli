// Package status implements "glinet-cli status": router overview.
package status

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/antavik/glinet-cli/src/cmd"
	"github.com/antavik/glinet-cli/src/internal/glinet"
)

func init() {
	cmd.Register(cmd.Command{
		Name:  "status",
		Usage: []string{"status\tshow router overview"},
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
	return cmd.WithClient(printStatus), nil
}

// printStatus prints the router overview. Info and Status are hard: an error
// fails the command before anything is printed. CheckFirmware, WanStatus and
// Tunnels are best-effort; each degrades its own line to "unavailable".
func printStatus(ctx context.Context, c *glinet.Client, stdio cmd.IO) error {
	info, err := c.Info(ctx)
	if err != nil {
		return err
	}
	st, err := c.Status(ctx)
	if err != nil {
		return err
	}

	upd, updErr := c.CheckFirmware(ctx)
	wan, wanErr := c.WanStatus(ctx)
	tunnels, tunErr := c.Tunnels(ctx)

	fmt.Fprintf(stdio.Out, "Model: %s\n", info.Model)
	fmt.Fprintf(stdio.Out, "Hostname: %s\n", info.Hostname)
	fmt.Fprintf(stdio.Out, "MAC: %s\n", info.MAC)
	firmware := info.FirmwareVersion
	if updErr == nil && upd.NewVersion != "" {
		firmware += " (update available: " + upd.NewVersion + ")"
	}
	fmt.Fprintf(stdio.Out, "Firmware: %s\n", firmware)
	fmt.Fprintf(stdio.Out, "Uptime: %s\n", formatUptime(st.Uptime))
	fmt.Fprintf(stdio.Out, "Load: %.2f %.2f %.2f\n", st.LoadAverage[0], st.LoadAverage[1], st.LoadAverage[2])

	var memUsed uint64
	if st.MemoryFree < st.MemoryTotal {
		memUsed = st.MemoryTotal - st.MemoryFree
	}
	memPct := 0
	if st.MemoryTotal > 0 {
		memPct = int(float64(memUsed)*100/float64(st.MemoryTotal) + 0.5)
	}
	fmt.Fprintf(stdio.Out, "Memory: %d%% (%s / %s)\n", memPct, formatBytes(memUsed), formatBytes(st.MemoryTotal))

	var flUsed uint64
	if st.FlashFree < st.FlashTotal {
		flUsed = st.FlashTotal - st.FlashFree
	}
	flPct := 0
	if st.FlashTotal > 0 {
		flPct = int(float64(flUsed)*100/float64(st.FlashTotal) + 0.5)
	}
	fmt.Fprintf(stdio.Out, "Flash: %d%% (%s / %s)\n", flPct, formatBytes(flUsed), formatBytes(st.FlashTotal))

	switch {
	case wanErr != nil:
		fmt.Fprintln(stdio.Out, "Ethernet: unavailable")
	case wan.Status == 1:
		line := wan.Protocol + " " + wan.IPv4.IP + " gw " + wan.IPv4.Gateway
		if len(wan.IPv4.DNS) > 0 {
			line += " dns " + strings.Join(wan.IPv4.DNS, ",")
		}
		fmt.Fprintln(stdio.Out, "Ethernet: "+line+" (connected)")
	case wan.Status == 2:
		fmt.Fprintf(stdio.Out, "Ethernet: %s (connecting)\n", wan.Protocol)
	case wan.Status == 0:
		fmt.Fprintf(stdio.Out, "Ethernet: %s (disconnected)\n", wan.Protocol)
	case wan.Status == 3:
		fmt.Fprintf(stdio.Out, "Ethernet: %s (no cable)\n", wan.Protocol)
	default:
		fmt.Fprintf(stdio.Out, "Ethernet: %s (status %d)\n", wan.Protocol, wan.Status)
	}

	if tunErr != nil {
		fmt.Fprintln(stdio.Out, "VPN: unavailable")
	} else {
		var parts []string
		for _, t := range tunnels {
			if !t.Enabled {
				continue
			}
			word := "disconnected"
			switch t.Status {
			case 1:
				word = "connected"
			case 2:
				word = "connecting"
			}
			parts = append(parts, t.Name+" "+word)
		}
		if len(parts) == 0 {
			fmt.Fprintln(stdio.Out, "VPN: none enabled")
		} else {
			fmt.Fprintln(stdio.Out, "VPN: "+strings.Join(parts, ", "))
		}
	}
	return nil
}

// formatUptime renders d as days, hours and minutes, e.g. "3d 4h 5m".
func formatUptime(d time.Duration) string {
	day := 24 * time.Hour
	return fmt.Sprintf("%dd %dh %dm", d/day, d%day/time.Hour, d%time.Hour/time.Minute)
}

// formatBytes renders b in binary units: whole bytes below 1 KiB, integer
// KiB and MiB, one decimal GiB. Fractions round half away from zero.
func formatBytes(b uint64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%d B", b)
	case b < 1<<20:
		return fmt.Sprintf("%d KiB", (b+512)/1024)
	case b < 1<<30:
		return fmt.Sprintf("%d MiB", (b+(1<<19))/(1<<20))
	default:
		return fmt.Sprintf("%.1f GiB", float64(b)/(1<<30))
	}
}
