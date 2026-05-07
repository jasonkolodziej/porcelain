// Package networkmanager wraps org.freedesktop.NetworkManager. The current
// surface is intentionally narrow — only what the modules display — and is
// expected to grow as Phase 3 integrations land.
package networkmanager

import (
	"context"
	"fmt"

	godbus "github.com/godbus/dbus/v5"
)

const (
	dest      = "org.freedesktop.NetworkManager"
	rootPath  = "/org/freedesktop/NetworkManager"
	ifaceRoot = "org.freedesktop.NetworkManager"
	ifaceDev  = "org.freedesktop.NetworkManager.Device"
	props     = "org.freedesktop.DBus.Properties"
)

// Device summarises a single managed interface for the dashboard.
type Device struct {
	Path        godbus.ObjectPath
	Interface   string
	DeviceType  uint32
	State       uint32
	StateReason uint32
	HwAddress   string
}

// Client wraps a godbus connection scoped to NetworkManager.
type Client struct{ conn *godbus.Conn }

// New builds a Client around an already-connected bus.
func New(conn *godbus.Conn) *Client { return &Client{conn: conn} }

// ListDevices enumerates every managed device. Property reads are sequential
// because the surface is small; switch to GetManagedObjects if it grows.
func (c *Client) ListDevices(ctx context.Context) ([]Device, error) {
	if c == nil || c.conn == nil {
		return nil, fmt.Errorf("networkmanager: nil connection")
	}

	root := c.conn.Object(dest, rootPath)
	call := root.CallWithContext(ctx, ifaceRoot+".GetDevices", 0)
	if call.Err != nil {
		return nil, fmt.Errorf("networkmanager GetDevices: %w", call.Err)
	}
	var paths []godbus.ObjectPath
	if err := call.Store(&paths); err != nil {
		return nil, fmt.Errorf("networkmanager GetDevices decode: %w", err)
	}

	out := make([]Device, 0, len(paths))
	for _, p := range paths {
		obj := c.conn.Object(dest, p)
		d := Device{Path: p}

		_ = readProp(ctx, obj, ifaceDev, "Interface", &d.Interface)
		_ = readProp(ctx, obj, ifaceDev, "DeviceType", &d.DeviceType)
		_ = readProp(ctx, obj, ifaceDev, "State", &d.State)
		_ = readProp(ctx, obj, ifaceDev, "StateReason", &d.StateReason)
		_ = readProp(ctx, obj, ifaceDev, "HwAddress", &d.HwAddress)

		out = append(out, d)
	}
	return out, nil
}

// readProp pulls a single property value into dst.
func readProp(ctx context.Context, obj godbus.BusObject, iface, name string, dst any) error {
	v, err := obj.GetProperty(iface + "." + name)
	if err != nil {
		_ = ctx
		return err
	}
	return v.Store(dst)
}
