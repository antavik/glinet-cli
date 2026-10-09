package status

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/antavik/glinet-cli/src/cmd"
	"github.com/antavik/glinet-cli/src/internal/glinet"
)

func init() {
	cmd.Register(cmd.Command{
		Name:  "status",
		Usage: []string{"status [-json]\tshow router overview"},
		Parse: parse,
	})
}

func parse(fs *flag.FlagSet, args []string) (cmd.Action, error) {
	asJSON := fs.Bool("json", false, "print status as JSON")
	a, err := cmd.ParseArgs(fs, args)
	if err != nil {
		return nil, err
	}
	if a.Sub != "" {
		return nil, errors.New("status takes no arguments")
	}
	return cmd.WithClient(func(ctx context.Context, c *glinet.Client, stdio cmd.IO) error {
		o, err := fetch(ctx, c)
		if err != nil {
			return err
		}
		o.warn(stdio.Err)
		if *asJSON {
			return printJSON(stdio.Out, o)
		}
		return printText(stdio.Out, o)
	}), nil
}

type overview struct {
	info    glinet.DeviceInfo
	st      glinet.SystemStatus
	upd     glinet.FirmwareUpdate
	updErr  error
	wan     glinet.WanStatus
	wanErr  error
	tunnels []glinet.Tunnel
	tunErr  error
}

func fetch(ctx context.Context, c *glinet.Client) (overview, error) {
	var o overview
	var err error
	if o.info, err = c.Info(ctx); err != nil {
		return overview{}, err
	}
	if o.st, err = c.Status(ctx); err != nil {
		return overview{}, err
	}

	// Optional calls run in parallel under the command's -timeout deadline.
	// One that fails drops its row instead of failing the command.
	var wg sync.WaitGroup
	wg.Go(func() { o.upd, o.updErr = c.CheckFirmware(ctx) })
	wg.Go(func() { o.wan, o.wanErr = c.WanStatus(ctx) })
	wg.Go(func() { o.tunnels, o.tunErr = c.Tunnels(ctx) })
	wg.Wait()
	return o, nil
}

func (o overview) warn(w io.Writer) {
	for _, err := range []error{o.updErr, o.wanErr, o.tunErr} {
		if err != nil && !errors.Is(err, glinet.ErrNoWAN) {
			fmt.Fprintf(w, "glinet-cli: warning: %v\n", err)
		}
	}
}

func printText(w io.Writer, o overview) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Router\t%s  %s  %s\n", o.info.Hostname, o.info.Model, o.info.MAC)
	fmt.Fprintf(tw, "Firmware\t%s\n", firmwareText(o))
	fmt.Fprintf(tw, "Uptime\t%s\n", formatUptime(o.st.Uptime))
	fmt.Fprintf(tw, "Load\t%.2f %.2f %.2f\n", o.st.LoadAverage[0], o.st.LoadAverage[1], o.st.LoadAverage[2])
	mem, flash := memoryUsage(o.st), flashUsage(o.st)
	fmt.Fprintf(tw, "Memory\t%d%%  %s / %s\n", mem.UsedPercent, formatBytes(mem.UsedBytes), formatBytes(mem.TotalBytes))
	fmt.Fprintf(tw, "Flash\t%d%%  %s / %s\n", flash.UsedPercent, formatBytes(flash.UsedBytes), formatBytes(flash.TotalBytes))
	fmt.Fprintf(tw, "WAN\t%s\n", wanText(o))
	fmt.Fprintf(tw, "VPN\t%s\n", vpnText(o))
	return tw.Flush()
}

func firmwareText(o overview) string {
	v := o.info.FirmwareVersion
	switch {
	case o.updErr != nil:
		return v + " (update check failed)"
	case o.upd.NewVersion != "":
		return v + " (update " + o.upd.NewVersion + " available)"
	}
	return v + " (up to date)"
}

func wanText(o overview) string {
	online := onlineUplinks(o.st.Networks)
	if len(online) > 0 {
		s := "online via " + strings.Join(online, ", ")
		if o.wanErr == nil && o.wan.Status == 1 && slices.Contains(online, "cable") {
			s += "  " + cableDetail(o.wan)
		}
		return s
	}

	var s string
	switch {
	case o.wanErr == nil:
		s = "cable " + cableState(o.wan.Status)
		if o.wan.Status == 1 {
			s += "  " + cableDetail(o.wan)
		}
	case errors.Is(o.wanErr, glinet.ErrNoWAN):
		s = "no cable WAN"
	default:
		s = "cable unknown"
	}
	if online == nil {
		return s
	}
	return "offline  " + s
}

var uplinkNames = map[string]string{"wan": "cable", "wwan": "repeater", "tethering": "tethering"}

func onlineUplinks(nets []glinet.Network) []string {
	if len(nets) == 0 {
		return nil
	}
	online := []string{}
	for _, n := range nets {
		if name, ok := uplinkNames[n.Interface]; ok && n.Online {
			online = append(online, name)
		}
	}
	return online
}

func cableState(status int) string {
	switch status {
	case 0:
		return "disconnected"
	case 1:
		return "connected"
	case 2:
		return "connecting"
	case 3:
		return "no cable"
	}
	return fmt.Sprintf("status %d", status)
}

func cableDetail(wan glinet.WanStatus) string {
	s := wan.Protocol + " " + wan.IPv4.IP + "  gw " + wan.IPv4.Gateway
	if len(wan.IPv4.DNS) > 0 {
		s += "  dns " + strings.Join(wan.IPv4.DNS, ", ")
	}
	return s
}

func vpnText(o overview) string {
	if o.tunErr != nil {
		return "unknown"
	}
	var parts []string
	for _, t := range o.tunnels {
		if t.Enabled {
			parts = append(parts, fmt.Sprintf("%s #%d %s", t.Name, t.ID, tunnelState(t)))
		}
	}
	if len(parts) == 0 {
		return "none enabled"
	}
	return strings.Join(parts, ", ")
}

func tunnelState(t glinet.Tunnel) string {
	switch {
	case !t.Enabled:
		return "off"
	case t.Status == 1:
		return "connected"
	case t.Status == 2:
		return "connecting"
	}
	return "disconnected"
}

type usage struct {
	TotalBytes  uint64 `json:"total_bytes"`
	UsedBytes   uint64 `json:"used_bytes"`
	UsedPercent int    `json:"used_percent"`
}

func newUsage(total, avail uint64) usage {
	u := usage{TotalBytes: total}
	if avail < total {
		u.UsedBytes = total - avail
	}
	if total > 0 {
		u.UsedPercent = int(float64(u.UsedBytes)*100/float64(total) + 0.5)
	}
	return u
}

func memoryUsage(st glinet.SystemStatus) usage {
	return newUsage(st.MemoryTotal, st.MemoryFree+st.MemoryBuffCache)
}

func flashUsage(st glinet.SystemStatus) usage {
	return newUsage(st.FlashTotal, st.FlashFree)
}

type report struct {
	Router struct {
		Model    string `json:"model"`
		Hostname string `json:"hostname"`
		MAC      string `json:"mac"`
	} `json:"router"`
	Firmware struct {
		Version string  `json:"version"`
		Update  *string `json:"update"` // null when up to date or unknown
		Error   string  `json:"error,omitempty"`
	} `json:"firmware"`
	UptimeSeconds int64      `json:"uptime_seconds"`
	LoadAverage   [3]float64 `json:"load_average"`
	Memory        usage      `json:"memory"`
	Flash         usage      `json:"flash"`
	WAN           struct {
		Online []string   `json:"online"` // null when the router lists no uplinks
		Cable  *cableJSON `json:"cable"`  // null without a cable WAN
		Error  string     `json:"error,omitempty"`
	} `json:"wan"`
	VPN struct {
		Tunnels []tunnelJSON `json:"tunnels"`
		Error   string       `json:"error,omitempty"`
	} `json:"vpn"`
}

type cableJSON struct {
	Protocol string   `json:"protocol"`
	Status   string   `json:"status"`
	IP       string   `json:"ip"`
	Gateway  string   `json:"gateway"`
	DNS      []string `json:"dns"`
}

type tunnelJSON struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"`
}

func printJSON(w io.Writer, o overview) error {
	var r report
	r.Router.Model, r.Router.Hostname, r.Router.MAC = o.info.Model, o.info.Hostname, o.info.MAC
	r.Firmware.Version = o.info.FirmwareVersion
	if o.updErr != nil {
		r.Firmware.Error = o.updErr.Error()
	} else if o.upd.NewVersion != "" {
		r.Firmware.Update = &o.upd.NewVersion
	}
	r.UptimeSeconds = int64(o.st.Uptime / time.Second)
	r.LoadAverage = o.st.LoadAverage
	r.Memory, r.Flash = memoryUsage(o.st), flashUsage(o.st)

	r.WAN.Online = onlineUplinks(o.st.Networks)
	switch {
	case o.wanErr == nil:
		dns := o.wan.IPv4.DNS
		if dns == nil {
			dns = []string{}
		}
		r.WAN.Cable = &cableJSON{
			Protocol: o.wan.Protocol,
			Status:   cableState(o.wan.Status),
			IP:       o.wan.IPv4.IP,
			Gateway:  o.wan.IPv4.Gateway,
			DNS:      dns,
		}
	case !errors.Is(o.wanErr, glinet.ErrNoWAN):
		r.WAN.Error = o.wanErr.Error()
	}

	if o.tunErr != nil {
		r.VPN.Error = o.tunErr.Error()
	} else {
		r.VPN.Tunnels = []tunnelJSON{}
		for _, t := range o.tunnels {
			r.VPN.Tunnels = append(r.VPN.Tunnels, tunnelJSON{ID: t.ID, Name: t.Name, Enabled: t.Enabled, Status: tunnelState(t)})
		}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func formatUptime(d time.Duration) string {
	day := 24 * time.Hour
	days, hours, mins := d/day, d%day/time.Hour, d%time.Hour/time.Minute
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func formatBytes(b uint64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%d B", b)
	case b < 1<<20-1<<9:
		return fmt.Sprintf("%d KiB", (b+512)/1024)
	case b < 1<<30-1<<19:
		return fmt.Sprintf("%d MiB", (b+(1<<19))/(1<<20))
	default:
		return fmt.Sprintf("%.1f GiB", float64(b)/(1<<30))
	}
}
