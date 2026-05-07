// Package zfs exposes a sidebar-visible Module summarising local ZFS state.
// The first slice only reports presence of the zpool/zfs CLIs; pool /
// dataset rendering arrives in a later phase.
package zfs

import (
	"context"
	"os/exec"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules"
)

// Module implements modules.Module for the ZFS sub-page under Storage.
type Module struct {
	mode string
}

// New returns a Module that probes for the zpool binary on PATH.
func New(_ context.Context) *Module {
	if _, err := exec.LookPath("zpool"); err == nil {
		return &Module{mode: "present"}
	}
	return &Module{mode: "fake"}
}

// ID implements modules.Module.
func (m *Module) ID() string { return "zfs" }

// Name implements modules.Module.
func (m *Module) Name() string { return "ZFS" }

// Status implements modules.Module.
func (m *Module) Status(_ context.Context) modules.Status {
	switch m.mode {
	case "present":
		return modules.Status{Health: modules.HealthOK, Detail: "zpool on PATH"}
	case "fake":
		return modules.Status{Health: modules.HealthDegraded, Detail: "zpool not installed"}
	default:
		return modules.Status{Health: modules.HealthUnavailable}
	}
}

// Close implements modules.Module.
func (m *Module) Close() error { return nil }
