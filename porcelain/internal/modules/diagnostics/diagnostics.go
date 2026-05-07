// Package diagnostics surfaces lightweight host health information for the
// dashboard. The first slice exposes hostname, kernel, uptime, and a
// running-services count derived from systemd; deeper integrations
// (rpm-ostree, journal capture) land in subsequent slices.
package diagnostics

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	internaldbus "github.com/jasonkolodziej/porcelain/porcelain/internal/dbus"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/dbus/systemd"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules"

	godbus "github.com/godbus/dbus/v5"
)

// Snapshot is the read-only host overview consumed by the dashboard.
type Snapshot struct {
	Hostname        string
	Kernel          string
	OS              string
	Uptime          time.Duration
	RunningServices int
	TotalServices   int
	Services        []ServiceSummary
}

// ServiceSummary is a flattened systemd unit summary suitable for the
// dashboard service list.
type ServiceSummary struct {
	Name        string
	Description string
	Active      bool
	Status      string
}

// Module implements modules.Module on top of an optional systemd client.
type Module struct {
	conn   *godbus.Conn
	client *systemd.Client
	mode   string
	note   string

	mu     sync.RWMutex
	cached Snapshot
}

// NewFromConnection wires a Module to the system bus when available, or to
// a stub implementation suitable for non-Linux developer workstations.
func NewFromConnection(ctx context.Context, mgr *internaldbus.ConnectionManager) *Module {
	conn, err := mgr.Dial(ctx)
	if err != nil {
		mode := "fake"
		note := ""
		if !errors.Is(err, internaldbus.ErrBusUnavailable) {
			note = err.Error()
		}
		return &Module{mode: mode, note: note}
	}
	return &Module{conn: conn, client: systemd.New(conn), mode: "systemd"}
}

// ID implements modules.Module.
func (m *Module) ID() string { return "diagnostics" }

// Name implements modules.Module.
func (m *Module) Name() string { return "Diagnostics" }

// Status implements modules.Module.
func (m *Module) Status(_ context.Context) modules.Status {
	switch m.mode {
	case "systemd":
		return modules.Status{Health: modules.HealthOK, Detail: "systemd connected"}
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
	if m.conn == nil {
		return nil
	}
	return m.conn.Close()
}

// Capture returns a fresh host snapshot.
func (m *Module) Capture(ctx context.Context) (Snapshot, error) {
	host, _ := os.Hostname()
	if host == "" {
		host = "porcelain"
	}

	snap := Snapshot{
		Hostname: host,
		Kernel:   runtime.GOOS + "/" + runtime.GOARCH,
		OS:       runtime.GOOS,
		Uptime:   readUptime(),
	}

	if m.mode != "systemd" || m.client == nil {
		// Provide a representative service for the developer fake.
		snap.Services = []ServiceSummary{{
			Name:        "porcelain.service",
			Description: "Porcelain control plane (developer fake)",
			Active:      true,
			Status:      "active",
		}}
		snap.RunningServices = 1
		snap.TotalServices = 1
		m.mu.Lock()
		m.cached = snap
		m.mu.Unlock()
		return snap, nil
	}

	units, err := m.client.ListUnits(ctx)
	if err != nil {
		return snap, fmt.Errorf("diagnostics: %w", err)
	}

	for _, u := range units {
		if !strings.HasSuffix(u.Name, ".service") {
			continue
		}
		snap.TotalServices++
		active := u.ActiveState == "active"
		if active {
			snap.RunningServices++
		}
		snap.Services = append(snap.Services, ServiceSummary{
			Name:        u.Name,
			Description: u.Description,
			Active:      active,
			Status:      u.ActiveState,
		})
	}

	m.mu.Lock()
	m.cached = snap
	m.mu.Unlock()
	return snap, nil
}

// readUptime returns the host uptime, falling back to zero on non-Linux.
func readUptime() time.Duration {
	raw, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	if i := bytes.IndexByte(raw, ' '); i > 0 {
		raw = raw[:i]
	}
	var seconds float64
	if _, err := fmt.Sscanf(string(raw), "%f", &seconds); err != nil {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}
