// Package storage owns the host-control storage feature: enumerating block
// devices via UDisks2, surfacing ZFS pools, and brokering the
// partition / encryption / RAID / NFS / iSCSI workflows that ship in later
// slices.
//
// The module exposes a Backend interface so the daemon can substitute a
// fake implementation on workstations that have no D-Bus (e.g. macOS) and
// still let the UI render meaningful data.
package storage

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	internaldbus "github.com/jasonkolodziej/porcelain/porcelain/internal/dbus"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/dbus/udisks2"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/viewdata"

	godbus "github.com/godbus/dbus/v5"
)

// Backend abstracts the data source the storage module renders from.
type Backend interface {
	// Snapshot returns a fresh view of the host's storage state.
	Snapshot(ctx context.Context) (viewdata.StorageData, error)
	// Kind is a label used to differentiate real vs fake backends in the UI.
	Kind() string
	// Close releases any underlying connections.
	Close() error
}

// Module implements modules.Module on top of a Backend.
type Module struct {
	backend Backend

	mu     sync.RWMutex
	cached viewdata.StorageData
	last   error
}

// New wires a Module to the supplied Backend.
func New(b Backend) *Module {
	return &Module{backend: b}
}

// NewFromConnection picks the most capable backend for the runtime: if the
// system bus is reachable a UDisks2-backed implementation is used, otherwise
// a fake suitable for macOS development is returned.
func NewFromConnection(ctx context.Context, mgr *internaldbus.ConnectionManager) *Module {
	conn, err := mgr.Dial(ctx)
	if err != nil {
		// Any dial error is recoverable for the UI; fall back to the fake but
		// record the cause via the module status.
		fb := NewFakeBackend()
		fb.note = err.Error()
		return New(fb)
	}
	return New(NewUDisks2Backend(conn))
}

// ID implements modules.Module.
func (m *Module) ID() string { return "storage" }

// Name implements modules.Module.
func (m *Module) Name() string { return "Storage" }

// Status implements modules.Module.
func (m *Module) Status(ctx context.Context) modules.Status {
	if m == nil || m.backend == nil {
		return modules.Status{Health: modules.HealthUnavailable, Detail: "no backend"}
	}

	switch m.backend.Kind() {
	case "udisks2":
		return modules.Status{Health: modules.HealthOK, Detail: "UDisks2 connected"}
	case "fake":
		detail := "developer fake (no D-Bus)"
		if fb, ok := m.backend.(*fakeBackend); ok && fb != nil && fb.note != "" {
			detail = fb.note
		}
		return modules.Status{Health: modules.HealthDegraded, Detail: detail}
	default:
		return modules.Status{Health: modules.HealthDegraded, Detail: m.backend.Kind()}
	}
}

// Close implements modules.Module.
func (m *Module) Close() error {
	if m == nil || m.backend == nil {
		return nil
	}
	return m.backend.Close()
}

// Snapshot returns a cached view if available; otherwise it fetches a new one.
func (m *Module) Snapshot(ctx context.Context) (viewdata.StorageData, error) {
	if m == nil || m.backend == nil {
		return viewdata.StorageData{}, fmt.Errorf("storage: no backend")
	}

	data, err := m.backend.Snapshot(ctx)

	m.mu.Lock()
	if err == nil {
		m.cached = data
		m.last = nil
	} else {
		m.last = err
	}
	m.mu.Unlock()

	if err != nil {
		// Return cached data if available so the UI degrades gracefully.
		m.mu.RLock()
		cached := m.cached
		m.mu.RUnlock()
		return cached, err
	}
	return data, nil
}

// FormatBlock formats a UDisks2 block object to the requested filesystem type.
func (m *Module) FormatBlock(ctx context.Context, objectPath, fsType string) error {
	if m == nil {
		return fmt.Errorf("storage: module is nil")
	}
	b, ok := m.backend.(*udisks2Backend)
	if !ok || b == nil || b.client == nil {
		return fmt.Errorf("storage: udisks2 backend unavailable")
	}
	return b.client.FormatBlock(ctx, godbus.ObjectPath(objectPath), fsType)
}

// UnlockBlock unlocks an encrypted UDisks2 block object.
func (m *Module) UnlockBlock(ctx context.Context, objectPath, passphrase string) error {
	if m == nil {
		return fmt.Errorf("storage: module is nil")
	}
	b, ok := m.backend.(*udisks2Backend)
	if !ok || b == nil || b.client == nil {
		return fmt.Errorf("storage: udisks2 backend unavailable")
	}
	return b.client.UnlockBlock(ctx, godbus.ObjectPath(objectPath), passphrase)
}

// FormatDevice formats a block device path via udisksctl.
func (m *Module) FormatDevice(ctx context.Context, devicePath, fsType string) error {
	devicePath = strings.TrimSpace(devicePath)
	if devicePath == "" {
		return fmt.Errorf("device path is required")
	}
	fsType = strings.TrimSpace(strings.ToLower(fsType))
	if fsType == "" {
		fsType = "ext4"
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(cmdCtx, "udisksctl", "format", "-b", devicePath, "--type", fsType).CombinedOutput()
	if err != nil {
		return fmt.Errorf("udisksctl format failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// UnlockDevice unlocks an encrypted block device using udisksctl.
func (m *Module) UnlockDevice(ctx context.Context, devicePath string) error {
	devicePath = strings.TrimSpace(devicePath)
	if devicePath == "" {
		return fmt.Errorf("device path is required")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cmdCtx, "udisksctl", "unlock", "-b", devicePath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("udisksctl unlock failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// udisks2Backend is the real backend backed by org.freedesktop.UDisks2.
type udisks2Backend struct {
	conn   *godbus.Conn
	client *udisks2.Client
}

// NewUDisks2Backend constructs a Backend that owns the supplied connection.
func NewUDisks2Backend(conn *godbus.Conn) Backend {
	return &udisks2Backend{conn: conn, client: udisks2.New(conn)}
}

func (b *udisks2Backend) Kind() string { return "udisks2" }

func (b *udisks2Backend) Close() error {
	if b.conn == nil {
		return nil
	}
	return b.conn.Close()
}

func (b *udisks2Backend) Snapshot(ctx context.Context) (viewdata.StorageData, error) {
	devices, err := b.client.ListBlockDevices(ctx)
	if err != nil {
		return viewdata.StorageData{}, err
	}

	out := viewdata.StorageData{
		BlockDevices:     make([]viewdata.BlockDevice, 0, len(devices)),
		AvailableDevices: make([]viewdata.BlockDevice, 0),
	}
	for _, d := range devices {
		if d.HintIgnore {
			continue
		}
		bd := viewdata.BlockDevice{
			ObjectPath:     string(d.ObjectPath),
			Device:         d.Device,
			Path:           d.Device,
			Model:          firstNonEmpty(d.DriveModel, d.DriveVendor),
			Size:           d.Size,
			Type:           classify(d),
			Filesystem:     d.IDType,
			IsEncrypted:    d.IsEncrypted,
			EncryptionType: d.EncryptionType,
		}
		out.BlockDevices = append(out.BlockDevices, bd)
		if d.IDType == "" && !d.IsEncrypted {
			out.AvailableDevices = append(out.AvailableDevices, bd)
		}
	}
	// ZFS pools land in the dedicated zfs module in a later slice; the
	// storage page renders an empty grid until then.
	out.Pools = []viewdata.Pool{}
	return out, nil
}

func classify(d udisks2.BlockDevice) string {
	switch d.IDUsage {
	case "filesystem":
		return "filesystem"
	case "crypto":
		return "encrypted"
	case "raid":
		return "raid"
	case "":
		return "raw"
	default:
		return d.IDUsage
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
