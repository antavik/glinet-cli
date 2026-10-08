package glinet

import (
	"context"
	"errors"
	"fmt"
)

// FirmwareUpdate is the result of an online firmware check.
type FirmwareUpdate struct {
	NewVersion string // "" when up to date
}

// CheckFirmware asks the router to look online for a newer firmware. The
// router needs internet access for it.
func (c *Client) CheckFirmware(ctx context.Context) (FirmwareUpdate, error) {
	var res struct {
		CurrentVersion string `json:"current_version"`
		VersionNew     string `json:"version_new"`
		ErrCode        int    `json:"err_code"`
	}
	if err := c.call(ctx, "upgrade", "check_firmware_online", nil, &res); err != nil {
		return FirmwareUpdate{}, err
	}
	if res.ErrCode < 0 {
		return FirmwareUpdate{}, fmt.Errorf("firmware check failed (err_code %d)", res.ErrCode)
	}
	if res.CurrentVersion == "" {
		return FirmwareUpdate{}, errors.New("firmware check returned no current version")
	}
	return FirmwareUpdate{NewVersion: printable(res.VersionNew)}, nil
}
