// Package systemd is a thin wrapper around the org.freedesktop.systemd1
// D-Bus interface. It exposes only the operations the host-control modules
// need today and intentionally avoids pulling in coreos/go-systemd so the
// daemon stays buildable on macOS workstations.
package systemd

import (
	"context"
	"fmt"

	godbus "github.com/godbus/dbus/v5"
)

const (
	dest = "org.freedesktop.systemd1"
	path = "/org/freedesktop/systemd1"
	mgr  = "org.freedesktop.systemd1.Manager"
)

// Unit is a single systemd unit summary as returned by ListUnits.
type Unit struct {
	Name        string
	Description string
	LoadState   string
	ActiveState string
	SubState    string
	Followed    string
	UnitPath    godbus.ObjectPath
	JobID       uint32
	JobType     string
	JobPath     godbus.ObjectPath
}

// Client wraps a godbus connection scoped to systemd.
type Client struct {
	conn *godbus.Conn
}

// New builds a Client around an already-connected, authenticated bus.
func New(conn *godbus.Conn) *Client {
	return &Client{conn: conn}
}

// ListUnits returns the currently loaded units. Callers can filter by name
// suffix (e.g. ".service") to scope the result set.
func (c *Client) ListUnits(ctx context.Context) ([]Unit, error) {
	if c == nil || c.conn == nil {
		return nil, fmt.Errorf("systemd: nil connection")
	}

	obj := c.conn.Object(dest, path)
	call := obj.CallWithContext(ctx, mgr+".ListUnits", 0)
	if call.Err != nil {
		return nil, fmt.Errorf("systemd ListUnits: %w", call.Err)
	}

	var raw [][]any
	if err := call.Store(&raw); err != nil {
		return nil, fmt.Errorf("systemd ListUnits decode: %w", err)
	}

	units := make([]Unit, 0, len(raw))
	for _, row := range raw {
		if len(row) < 10 {
			continue
		}
		u := Unit{}
		u.Name, _ = row[0].(string)
		u.Description, _ = row[1].(string)
		u.LoadState, _ = row[2].(string)
		u.ActiveState, _ = row[3].(string)
		u.SubState, _ = row[4].(string)
		u.Followed, _ = row[5].(string)
		u.UnitPath, _ = row[6].(godbus.ObjectPath)
		if v, ok := row[7].(uint32); ok {
			u.JobID = v
		}
		u.JobType, _ = row[8].(string)
		u.JobPath, _ = row[9].(godbus.ObjectPath)
		units = append(units, u)
	}
	return units, nil
}
