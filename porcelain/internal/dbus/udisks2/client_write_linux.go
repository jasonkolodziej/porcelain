//go:build linux

package udisks2

import (
	"context"
	"fmt"
	"strings"

	godbus "github.com/godbus/dbus/v5"
)

// bootMountPaths are active mount points that indicate the currently booted
// system depends on the device. Formatting any device that holds one of these
// is refused.
var bootMountPaths = map[string]bool{
	"/":         true,
	"/boot":     true,
	"/boot/efi": true,
}

// CheckNotBootDevice returns an error when objectPath is one of the active
// boot or root filesystems of the running OS. Call this before FormatBlock.
func (c *Client) CheckNotBootDevice(ctx context.Context, objectPath godbus.ObjectPath) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("udisks2: nil connection")
	}

	obj := c.conn.Object(dest, objectPath)
	dbusProps := "org.freedesktop.DBus.Properties"

	// Check MountPoints on the Filesystem interface (not present on encrypted/raw devices).
	var fsVariant godbus.Variant
	call := obj.CallWithContext(ctx, dbusProps+".Get", 0, ifaceFS, "MountPoints")
	if call.Err == nil {
		if err := call.Store(&fsVariant); err == nil {
			if mps, ok := fsVariant.Value().([][]byte); ok {
				for _, mp := range mps {
					mountPath := strings.TrimRight(string(mp), "\x00")
					if bootMountPaths[mountPath] {
						return fmt.Errorf("storage: refusing to format device %s mounted at %s", objectPath, mountPath)
					}
				}
			}
		}
	}

	return nil
}

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

// UnlockBlock unlocks an encrypted block device via org.freedesktop.UDisks2.Encrypted.Unlock.
// passphrase may be empty for devices using a keyfile or TPM unlock.
func (c *Client) UnlockBlock(ctx context.Context, objectPath godbus.ObjectPath, passphrase string) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("udisks2: nil connection")
	}
	if objectPath == "" || objectPath == "/" {
		return fmt.Errorf("udisks2: block object path is required")
	}

	obj := c.conn.Object(dest, objectPath)
	options := map[string]godbus.Variant{}
	var cleartextPath godbus.ObjectPath
	call := obj.CallWithContext(ctx, ifaceCrypto+".Unlock", 0, passphrase, options)
	if call.Err != nil {
		return fmt.Errorf("udisks2 unlock %s: %w", objectPath, call.Err)
	}
	if err := call.Store(&cleartextPath); err != nil {
		return fmt.Errorf("udisks2 unlock %s: reading cleartext path: %w", objectPath, err)
	}
	return nil
}
