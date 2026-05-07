// Package firewall exposes a sidebar-visible Module summarising firewalld
// state. Detailed zone / rule rendering lands in a later phase.
package firewall

import (
	"context"
	"errors"
	"fmt"

	internaldbus "github.com/jasonkolodziej/porcelain/porcelain/internal/dbus"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/dbus/firewalld"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules"

	godbus "github.com/godbus/dbus/v5"
)

// Module implements modules.Module for the Firewall sub-page under Network.
type Module struct {
	conn   *godbus.Conn
	client *firewalld.Client
	mode   string
	note   string
}

// NewFromConnection wires the Module against the system bus and falls back
// to a fake when the bus is unavailable (e.g. macOS dev).
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
	return &Module{conn: conn, client: firewalld.New(conn), mode: "firewalld"}
}

// ID implements modules.Module.
func (m *Module) ID() string { return "firewall" }

// Name implements modules.Module.
func (m *Module) Name() string { return "Firewall" }

// Status implements modules.Module.
func (m *Module) Status(_ context.Context) modules.Status {
	switch m.mode {
	case "firewalld":
		return modules.Status{Health: modules.HealthOK, Detail: "firewalld reachable"}
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

// Summary returns the current firewalld zone/service overview.
func (m *Module) Summary(ctx context.Context) (firewalld.ZoneSummary, error) {
	if m == nil || m.client == nil || m.mode != "firewalld" {
		return firewalld.ZoneSummary{}, fmt.Errorf("firewalld client unavailable")
	}
	return m.client.Summary(ctx)
}

// SetService enables or disables a named service in a zone.
func (m *Module) SetService(ctx context.Context, zone, service string, enabled bool) error {
	if m == nil || m.client == nil || m.mode != "firewalld" {
		return fmt.Errorf("firewalld client unavailable")
	}
	return m.client.SetServiceInZone(ctx, zone, service, enabled)
}
