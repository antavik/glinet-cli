package glinet

import (
	"context"
	"fmt"
)

// FirmwareUpdate holds the router's current firmware version and, when an
// update is available, the new version.
type FirmwareUpdate struct {
	CurrentVersion string
	NewVersion     string
}

// CheckFirmware reports the router's firmware version and any available
// update, with strings made safe to print. NewVersion is empty when the router
// is already current. The router reports a negative err_code as a field of the
// result payload when the check cannot run.
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
		return FirmwareUpdate{}, fmt.Errorf("firmware check returned no current version")
	}
	return FirmwareUpdate{
		CurrentVersion: printable(res.CurrentVersion),
		NewVersion:     printable(res.VersionNew),
	}, nil
}
