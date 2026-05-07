// Package network exposes a sidebar-visible Module summarising the host's
// network state. The first slice reports only connectivity to NetworkManager;
// detailed device / connection rendering lands in a later phase.
package network

import (
	"context"
	"errors"
	"fmt"

	internaldbus "github.com/jasonkolodziej/porcelain/porcelain/internal/dbus"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/dbus/networkmanager"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules"

	godbus "github.com/godbus/dbus/v5"
)

// Module implements modules.Module for the Network group landing page.
type Module struct {
	conn   *godbus.Conn
	client *networkmanager.Client
	mode   string
	note   string
}

// NewFromConnection returns a Module that prefers the system bus
// (NetworkManager) and falls back to a fake on dev workstations.
func NewFromConnection(ctx context.Context, mgr *internaldbus.ConnectionManager) *Module {
	if mgr == nil {
		return &Module{mode: "fake", note: "no connection manager"}
	}
	conn, err := mgr.Dial(ctx)
	if err != nil {
		if errors.Is(err, internaldbus.ErrBusUnavailable) {
			return &Module{mode: "fake"}
		}
		return &Module{mode: "fake", note: err.Error()}
	}
	return &Module{conn: conn, client: networkmanager.New(conn), mode: "networkmanager"}
}

// ID implements modules.Module.
func (m *Module) ID() string { return "network" }

// Name implements modules.Module.
func (m *Module) Name() string { return "Network" }

// Status implements modules.Module.
func (m *Module) Status(_ context.Context) modules.Status {
	switch m.mode {
	case "networkmanager":
		return modules.Status{Health: modules.HealthOK, Detail: "NetworkManager reachable"}
	case "fake":
		detail := "developer fake (no D-Bus)"
		if m.note != "" {
			detail = m.note
		}
		return modules.Status{Health: modules.HealthDegraded, Detail: detail}
	default:
		return modules.Status{Health: modules.HealthUnavailable}
	}
}

// Close implements modules.Module.
func (m *Module) Close() error {
	if m == nil || m.conn == nil {
		return nil
	}
	return m.conn.Close()
}

// Devices returns the current NetworkManager device list.
func (m *Module) Devices(ctx context.Context) ([]networkmanager.Device, error) {
	if m == nil || m.client == nil || m.mode != "networkmanager" {
		return nil, fmt.Errorf("networkmanager client unavailable")
	}
	return m.client.ListDevices(ctx)
}

// SetEnabled toggles global networking through NetworkManager.
func (m *Module) SetEnabled(ctx context.Context, enabled bool) error {
	if m == nil || m.client == nil || m.mode != "networkmanager" {
		return fmt.Errorf("networkmanager client unavailable")
	}
	return m.client.SetNetworkingEnabled(ctx, enabled)
}
