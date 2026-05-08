//go:build !linux

package udisks2

import (
	"context"
	"fmt"

	godbus "github.com/godbus/dbus/v5"
)

// FormatBlock is unsupported on non-Linux hosts.
func (c *Client) FormatBlock(_ context.Context, objectPath godbus.ObjectPath, fsType string) error {
	return fmt.Errorf("udisks2 format %s as %s unsupported on this platform", objectPath, fsType)
}

// UnlockBlock is unsupported on non-Linux hosts.
func (c *Client) UnlockBlock(_ context.Context, objectPath godbus.ObjectPath, _ string) error {
	return fmt.Errorf("udisks2 unlock %s unsupported on this platform", objectPath)
}
