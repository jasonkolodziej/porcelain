// Package udisks2 is a thin wrapper around the org.freedesktop.UDisks2
// D-Bus tree. It exposes only the read-only block-device enumeration
// the storage module needs today.
package udisks2

import (
	"context"
	"fmt"
	"sort"
	"strings"

	godbus "github.com/godbus/dbus/v5"
)

const (
	dest          = "org.freedesktop.UDisks2"
	rootPath      = "/org/freedesktop/UDisks2"
	objectManager = "org.freedesktop.DBus.ObjectManager"
	ifaceBlock    = "org.freedesktop.UDisks2.Block"
	ifaceFS       = "org.freedesktop.UDisks2.Filesystem"
	ifaceCrypto   = "org.freedesktop.UDisks2.Encrypted"
	ifaceDrive    = "org.freedesktop.UDisks2.Drive"
)

// BlockDevice is a flattened view of a single UDisks2 block object plus
// any sibling drive metadata that is useful for the dashboard.
type BlockDevice struct {
	ObjectPath     godbus.ObjectPath
	Device         string
	PreferredPath  string
	Size           int64
	IDType         string
	IDLabel        string
	IDUsage        string
	IsEncrypted    bool
	EncryptionType string
	HintIgnore     bool
	DriveModel     string
	DriveVendor    string
	DriveSerial    string
}

// Client wraps a godbus connection scoped to UDisks2.
type Client struct {
	conn *godbus.Conn
}

// New builds a Client around an already-connected, authenticated bus.
func New(conn *godbus.Conn) *Client {
	return &Client{conn: conn}
}

// ListBlockDevices returns every block object exposed by UDisks2.
func (c *Client) ListBlockDevices(ctx context.Context) ([]BlockDevice, error) {
	if c == nil || c.conn == nil {
		return nil, fmt.Errorf("udisks2: nil connection")
	}

	managed, err := c.managedObjects(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]BlockDevice, 0)
	for objPath, ifaces := range managed {
		blockProps, ok := ifaces[ifaceBlock]
		if !ok {
			continue
		}

		bd := BlockDevice{ObjectPath: objPath}
		bd.Device = bytesToString(blockProps["Device"])
		bd.PreferredPath = bytesToString(blockProps["PreferredDevice"])
		if v, ok := blockProps["Size"].Value().(uint64); ok {
			bd.Size = int64(v)
		}
		bd.IDType, _ = blockProps["IdType"].Value().(string)
		bd.IDLabel, _ = blockProps["IdLabel"].Value().(string)
		bd.IDUsage, _ = blockProps["IdUsage"].Value().(string)
		if v, ok := blockProps["HintIgnore"].Value().(bool); ok {
			bd.HintIgnore = v
		}

		if cryptoProps, ok := ifaces[ifaceCrypto]; ok {
			bd.IsEncrypted = true
			bd.EncryptionType, _ = cryptoProps["MetadataSize"].Value().(string)
			if bd.EncryptionType == "" {
				bd.EncryptionType = "LUKS"
			}
		}

		if drivePathV, ok := blockProps["Drive"]; ok {
			if drivePath, ok := drivePathV.Value().(godbus.ObjectPath); ok && drivePath != "/" {
				if drive, ok := managed[drivePath]; ok {
					if dp, ok := drive[ifaceDrive]; ok {
						bd.DriveModel, _ = dp["Model"].Value().(string)
						bd.DriveVendor, _ = dp["Vendor"].Value().(string)
						bd.DriveSerial, _ = dp["Serial"].Value().(string)
					}
				}
			}
		}

		out = append(out, bd)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Device < out[j].Device })
	return out, nil
}

// managedObjects calls org.freedesktop.DBus.ObjectManager.GetManagedObjects
// and returns the standard nested map.
func (c *Client) managedObjects(ctx context.Context) (
	map[godbus.ObjectPath]map[string]map[string]godbus.Variant, error,
) {
	obj := c.conn.Object(dest, rootPath)
	call := obj.CallWithContext(ctx, objectManager+".GetManagedObjects", 0)
	if call.Err != nil {
		return nil, fmt.Errorf("udisks2 GetManagedObjects: %w", call.Err)
	}
	var managed map[godbus.ObjectPath]map[string]map[string]godbus.Variant
	if err := call.Store(&managed); err != nil {
		return nil, fmt.Errorf("udisks2 GetManagedObjects decode: %w", err)
	}
	return managed, nil
}

// bytesToString interprets a UDisks2 byte-array property (NUL-terminated) as
// a Go string.
func bytesToString(v godbus.Variant) string {
	switch raw := v.Value().(type) {
	case []byte:
		return strings.TrimRight(string(raw), "\x00")
	case string:
		return raw
	default:
		return ""
	}
}
