//go:build linux

package udisks2

import (
	"context"
	"fmt"

	godbus "github.com/godbus/dbus/v5"
)

// FormatBlock formats a block device with the requested filesystem type.
func (c *Client) FormatBlock(ctx context.Context, objectPath godbus.ObjectPath, fsType string) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("udisks2: nil connection")
	}
	if objectPath == "" || objectPath == "/" {
		return fmt.Errorf("udisks2: block object path is required")
	}
	if fsType == "" {
		return fmt.Errorf("udisks2: filesystem type is required")
	}

	obj := c.conn.Object(dest, objectPath)
	options := map[string]godbus.Variant{}
	call := obj.CallWithContext(ctx, ifaceBlock+".Format", 0, fsType, options)
	if call.Err != nil {
		return fmt.Errorf("udisks2 format %s as %s: %w", objectPath, fsType, call.Err)
	}
	return nil
}
