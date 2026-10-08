package glinet

import (
	"context"
	"time"
)

// DeviceInfo identifies the router.
type DeviceInfo struct {
	Model           string
	MAC             string
	FirmwareVersion string
	Hostname        string
}

// Info returns the router's identity, with text made safe to print.
func (c *Client) Info(ctx context.Context) (DeviceInfo, error) {
	var res struct {
		Model           string `json:"model"`
		MAC             string `json:"mac"`
		FirmwareVersion string `json:"firmware_version"`
		BoardInfo       struct {
			Hostname string `json:"hostname"`
		} `json:"board_info"`
	}
	if err := c.call(ctx, "system", "get_info", nil, &res); err != nil {
		return DeviceInfo{}, err
	}
	return DeviceInfo{
		Model:           printable(res.Model),
		MAC:             printable(res.MAC),
		FirmwareVersion: printable(res.FirmwareVersion),
		Hostname:        printable(res.BoardInfo.Hostname),
	}, nil
}

// Network is one uplink interface, such as "wan" or "wwan".
type Network struct {
	Interface string
	Online    bool // internet is reachable through it
}

// SystemStatus is the router's uptime, load, memory, flash and uplinks.
type SystemStatus struct {
	Uptime          time.Duration
	LoadAverage     [3]float64
	MemoryTotal     uint64
	MemoryFree      uint64
	MemoryBuffCache uint64 // reclaimable; zero when the firmware omits it
	FlashTotal      uint64
	FlashFree       uint64
	Networks        []Network // empty when the firmware omits it
}

// Status returns the router's current system status.
func (c *Client) Status(ctx context.Context) (SystemStatus, error) {
	var res struct {
		Network []struct {
			Interface string `json:"interface"`
			Online    bool   `json:"online"`
		} `json:"network"`
		System struct {
			Uptime          float64    `json:"uptime"` // seconds
			LoadAverage     [3]float64 `json:"load_average"`
			MemoryTotal     uint64     `json:"memory_total"`
			MemoryFree      uint64     `json:"memory_free"`
			MemoryBuffCache uint64     `json:"memory_buff_cache"`
			FlashTotal      uint64     `json:"flash_total"`
			FlashFree       uint64     `json:"flash_free"`
		} `json:"system"`
	}
	if err := c.call(ctx, "system", "get_status", nil, &res); err != nil {
		return SystemStatus{}, err
	}
	var nets []Network
	for _, n := range res.Network {
		nets = append(nets, Network{Interface: printable(n.Interface), Online: n.Online})
	}
	return SystemStatus{
		Uptime:          time.Duration(res.System.Uptime * float64(time.Second)),
		LoadAverage:     res.System.LoadAverage,
		MemoryTotal:     res.System.MemoryTotal,
		MemoryFree:      res.System.MemoryFree,
		MemoryBuffCache: res.System.MemoryBuffCache,
		FlashTotal:      res.System.FlashTotal,
		FlashFree:       res.System.FlashFree,
		Networks:        nets,
	}, nil
}
