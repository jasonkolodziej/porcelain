// Package cloudflare exposes a sidebar-visible Module summarising the local
// cloudflared tunnel (if installed). The first slice only reports presence
// of the cloudflared binary; live tunnel state is added later.
package cloudflare

import (
	"context"
	"os/exec"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules"
)

// Module implements modules.Module for the Cloudflare sub-page under Network.
type Module struct {
	mode string
}

// New returns a Module that probes for cloudflared on PATH.
func New(_ context.Context) *Module {
	if _, err := exec.LookPath("cloudflared"); err == nil {
		return &Module{mode: "present"}
	}
	return &Module{mode: "fake"}
}

// ID implements modules.Module.
func (m *Module) ID() string { return "cloudflare" }

// Name implements modules.Module.
func (m *Module) Name() string { return "Cloudflare" }

// Status implements modules.Module.
func (m *Module) Status(_ context.Context) modules.Status {
	switch m.mode {
	case "present":
		return modules.Status{Health: modules.HealthOK, Detail: "cloudflared on PATH"}
	case "fake":
		return modules.Status{Health: modules.HealthDegraded, Detail: "cloudflared not installed"}
	default:
		return modules.Status{Health: modules.HealthUnavailable}
	}
}

// Close implements modules.Module.
func (m *Module) Close() error { return nil }
