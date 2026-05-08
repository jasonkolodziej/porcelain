// Package diagnostics surfaces lightweight host health information for the
// dashboard. The first slice exposes hostname, kernel, uptime, and a
// running-services count derived from systemd; deeper integrations
// (rpm-ostree, journal capture) land in subsequent slices.
package diagnostics

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
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
	JournalTail     []string
	OSTreeStatus    string
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
		return &Module{mode: "fake", note: err.Error()}
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
		snap.JournalTail = captureJournalTail(ctx, 20)
		snap.OSTreeStatus = captureOSTreeStatus(ctx)
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

	snap.JournalTail = captureJournalTail(ctx, 20)
	snap.OSTreeStatus = captureOSTreeStatus(ctx)

	m.mu.Lock()
	m.cached = snap
	m.mu.Unlock()
	return snap, nil
}

func captureJournalTail(ctx context.Context, lines int) []string {
	if lines <= 0 {
		lines = 20
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cmdCtx, "journalctl", "-n", fmt.Sprintf("%d", lines), "--no-pager", "-o", "short").CombinedOutput()
	if err != nil {
		return []string{"journalctl unavailable"}
	}
	raw := strings.Split(strings.TrimSpace(string(out)), "\n")
	list := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			list = append(list, line)
		}
	}
	if len(list) == 0 {
		return []string{"no journal entries"}
	}
	return list
}

func captureOSTreeStatus(ctx context.Context) string {
	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cmdCtx, "rpm-ostree", "status").CombinedOutput()
	if err != nil {
		return "rpm-ostree unavailable"
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return "rpm-ostree status empty"
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 6 {
		lines = lines[:6]
	}
	return strings.Join(lines, "\n")
}

// UnitAction executes a systemd unit action through the shared D-Bus client.
func (m *Module) UnitAction(ctx context.Context, unitName, action string) error {
	if m == nil || m.mode != "systemd" || m.client == nil {
		return fmt.Errorf("diagnostics: systemd client unavailable")
	}
	return m.client.UnitAction(ctx, unitName, action)
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
