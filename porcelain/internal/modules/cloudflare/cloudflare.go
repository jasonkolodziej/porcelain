// Package cloudflare exposes a sidebar-visible Module summarising the local
// cloudflared tunnel agent. Phase 4 adds live tunnel listing via
// `cloudflared tunnel list`.
package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/viewdata"
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

// Snapshot returns live tunnel information from cloudflared, or a degraded
// stub when cloudflared is not installed.
func (m *Module) Snapshot(ctx context.Context) (viewdata.CloudflarePageData, error) {
	data := viewdata.CloudflarePageData{
		Heading: "Cloudflare",
		Blurb:   "cloudflared tunnel agent and active tunnels",
	}
	if m == nil || m.mode == "fake" {
		data.Detail = "cloudflared not installed"
		return data, nil
	}

	version := cloudflaredVersion(ctx)
	data.Version = version

	tunnels, err := listTunnels(ctx)
	if err != nil {
		data.Detail = fmt.Sprintf("tunnel list failed: %v", err)
		return data, nil
	}
	data.Tunnels = tunnels
	if len(tunnels) == 0 {
		data.Detail = "cloudflared installed — no active tunnels"
	} else {
		data.Detail = fmt.Sprintf("%d tunnel(s) registered", len(tunnels))
	}
	return data, nil
}

// cloudflaredVersion returns the short version string or empty string on error.
func cloudflaredVersion(ctx context.Context) string {
	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cmdCtx, "cloudflared", "--version").Output()
	if err != nil {
		return ""
	}
	// output: "cloudflared version 2024.x.x (built ...)"
	line := strings.TrimSpace(string(out))
	parts := strings.Fields(line)
	if len(parts) >= 3 {
		return parts[2]
	}
	return line
}

// tunnelEntry mirrors the JSON emitted by `cloudflared tunnel list --output json`.
type tunnelEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	Status    string `json:"status"`
}

// listTunnels calls `cloudflared tunnel list --output json` and parses results.
// When credentials are unavailable cloudflared exits non-zero; we return an
// empty list rather than an error so the page still renders cleanly.
func listTunnels(ctx context.Context) ([]viewdata.CloudflareTunnel, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, "cloudflared", "tunnel", "list", "--output", "json").Output()
	if err != nil {
		// Exit non-zero usually means no credentials; treat as empty list.
		return []viewdata.CloudflareTunnel{}, nil
	}

	var entries []tunnelEntry
	if jsonErr := json.Unmarshal(out, &entries); jsonErr != nil {
		return nil, fmt.Errorf("parse tunnel list JSON: %w", jsonErr)
	}

	tunnels := make([]viewdata.CloudflareTunnel, 0, len(entries))
	for _, e := range entries {
		status := e.Status
		if status == "" {
			status = "inactive"
		}
		tunnels = append(tunnels, viewdata.CloudflareTunnel{
			ID:        e.ID,
			Name:      e.Name,
			CreatedAt: e.CreatedAt,
			Status:    status,
		})
	}
	return tunnels, nil
}
