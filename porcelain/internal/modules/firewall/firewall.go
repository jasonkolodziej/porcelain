// Package firewall exposes a sidebar-visible Module summarising firewalld
// state. Detailed zone / rule rendering lands in a later phase.
package firewall

import (
	"context"
	"errors"

	internaldbus "github.com/jasonkolodziej/porcelain/porcelain/internal/dbus"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules"
)

// Module implements modules.Module for the Firewall sub-page under Network.
type Module struct {
	mode string
	note string
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
	_ = conn.Close()
	return &Module{mode: "firewalld"}
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
func (m *Module) Close() error { return nil }
