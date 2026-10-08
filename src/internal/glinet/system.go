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

// Info returns the router's model, MAC, firmware version and hostname, with
// strings made safe to print.
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

// SystemStatus is the router's uptime, load and storage usage.
type SystemStatus struct {
	Uptime      time.Duration
	LoadAverage [3]float64
	MemoryTotal uint64
	MemoryFree  uint64
	FlashTotal  uint64
	FlashFree   uint64
}

// Status returns how long the router has been running, its load averages and
// its memory and flash usage.
func (c *Client) Status(ctx context.Context) (SystemStatus, error) {
	var res struct {
		System struct {
			Uptime      float64    `json:"uptime"` // seconds
			LoadAverage [3]float64 `json:"load_average"`
			MemoryTotal uint64     `json:"memory_total"`
			MemoryFree  uint64     `json:"memory_free"`
			FlashTotal  uint64     `json:"flash_total"`
			FlashFree   uint64     `json:"flash_free"`
		} `json:"system"`
	}
	if err := c.call(ctx, "system", "get_status", nil, &res); err != nil {
		return SystemStatus{}, err
	}
	return SystemStatus{
		Uptime:      time.Duration(res.System.Uptime * float64(time.Second)),
		LoadAverage: res.System.LoadAverage,
		MemoryTotal: res.System.MemoryTotal,
		MemoryFree:  res.System.MemoryFree,
		FlashTotal:  res.System.FlashTotal,
		FlashFree:   res.System.FlashFree,
	}, nil
}
