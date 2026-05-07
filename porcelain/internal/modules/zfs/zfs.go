// Package zfs exposes a sidebar-visible Module summarising local ZFS state.
// Phase 4 adds pool/dataset enumeration via zpool list and zfs list.
package zfs

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/viewdata"
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

// Snapshot returns live pool and dataset data, or a degraded stub when
// zpool is unavailable.
func (m *Module) Snapshot(ctx context.Context) (viewdata.ZFSPageData, error) {
	data := viewdata.ZFSPageData{
		Heading: "ZFS",
		Blurb:   "Pool health, dataset usage, and scrub status",
	}
	if m == nil || m.mode == "fake" {
		data.Detail = "zpool not installed"
		return data, nil
	}

	pools, err := listPools(ctx)
	if err != nil {
		data.Detail = fmt.Sprintf("zpool list failed: %v", err)
		return data, nil
	}

	for i := range pools {
		datasets, dsErr := listDatasets(ctx, pools[i].Name)
		if dsErr == nil {
			pools[i].Datasets = datasets
		}
		pools[i].ScrubStatus, pools[i].LastScrub = poolScrubStatus(ctx, pools[i].Name)
	}

	data.Pools = pools
	data.Detail = fmt.Sprintf("%d pool(s)", len(pools))
	return data, nil
}

// listPools parses `zpool list -H -o name,health,size,alloc,free,ratio,dedup`.
// -H disables the header and uses tab separators.
func listPools(ctx context.Context) ([]viewdata.ZFSPool, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, "zpool", "list", "-H",
		"-o", "name,health,size,alloc,free,ratio,dedup").Output()
	if err != nil {
		return nil, err
	}

	var pools []viewdata.ZFSPool
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 5 {
			continue
		}
		pool := viewdata.ZFSPool{
			Name:             cols[0],
			Health:           cols[1],
			CompressionRatio: "-",
			Deduplication:    "-",
		}
		if len(cols) >= 6 {
			pool.CompressionRatio = cols[5]
		}
		if len(cols) >= 7 {
			pool.Deduplication = cols[6]
		}
		pool.Size = parseZFSBytes(cols[2])
		pool.Allocated = parseZFSBytes(cols[3])
		pools = append(pools, pool)
	}
	return pools, nil
}

// listDatasets parses `zfs list -H -o name,used,avail -r <pool>`.
func listDatasets(ctx context.Context, pool string) ([]viewdata.ZFSDataset, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, "zfs", "list", "-H",
		"-o", "name,used,avail", "-r", pool).Output()
	if err != nil {
		return nil, err
	}

	var datasets []viewdata.ZFSDataset
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 3 {
			continue
		}
		datasets = append(datasets, viewdata.ZFSDataset{
			Name:      cols[0],
			Used:      parseZFSBytes(cols[1]),
			Available: parseZFSBytes(cols[2]),
		})
	}
	return datasets, nil
}

// poolScrubStatus parses the last scrub line from `zpool status -v <pool>`.
// Returns (statusText, lastScrubTime).
func poolScrubStatus(ctx context.Context, pool string) (string, string) {
	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, "zpool", "status", "-v", pool).Output()
	if err != nil {
		return "unknown", ""
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "scan:") {
			rest := strings.TrimPrefix(line, "scan:")
			rest = strings.TrimSpace(rest)
			if strings.Contains(rest, "scrub repaired") || strings.Contains(rest, "scrub in progress") {
				if idx := strings.Index(rest, " on "); idx >= 0 {
					return "scrubbed", strings.TrimSpace(rest[idx+4:])
				}
				return "scrubbed", ""
			}
			if strings.Contains(rest, "none requested") {
				return "none", ""
			}
			return rest, ""
		}
	}
	return "unknown", ""
}

// parseZFSBytes converts human-readable zpool/zfs sizes (e.g. "1.82T", "512M")
// into bytes. Returns 0 on parse failure or when the value is "-".
func parseZFSBytes(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return 0
	}
	if len(s) < 2 {
		return 0
	}
	suffix := strings.ToUpper(string(s[len(s)-1]))
	numStr := s[:len(s)-1]
	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0
	}
	switch suffix {
	case "K":
		return int64(val * 1024)
	case "M":
		return int64(val * 1024 * 1024)
	case "G":
		return int64(val * 1024 * 1024 * 1024)
	case "T":
		return int64(val * 1024 * 1024 * 1024 * 1024)
	case "P":
		return int64(val * 1024 * 1024 * 1024 * 1024 * 1024)
	default:
		// Bare number (bytes)
		return int64(val)
	}
}

// CreatePool creates a zpool with optional RAID topology and compression.
func (m *Module) CreatePool(ctx context.Context, name, raid, compression string, devices []string) error {
	if m == nil || m.mode == "fake" {
		return fmt.Errorf("zpool not installed")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("pool name is required")
	}
	if len(devices) == 0 {
		return fmt.Errorf("at least one device is required")
	}

	args := []string{"create", name}
	raid = strings.TrimSpace(strings.ToLower(raid))
	if raid != "" {
		args = append(args, raid)
	}
	for _, dev := range devices {
		dev = strings.TrimSpace(dev)
		if dev != "" {
			args = append(args, dev)
		}
	}

	createCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(createCtx, "zpool", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("zpool create failed: %s", strings.TrimSpace(string(out)))
	}

	compression = strings.TrimSpace(strings.ToLower(compression))
	if compression == "" {
		compression = "on"
	}
	setCtx, setCancel := context.WithTimeout(ctx, 15*time.Second)
	defer setCancel()
	setOut, setErr := exec.CommandContext(setCtx, "zfs", "set", "compression="+compression, name).CombinedOutput()
	if setErr != nil {
		return fmt.Errorf("pool created but compression set failed: %s", strings.TrimSpace(string(setOut)))
	}
	return nil
}

// DestroyPool destroys an existing zpool.
func (m *Module) DestroyPool(ctx context.Context, name string) error {
	if m == nil || m.mode == "fake" {
		return fmt.Errorf("zpool not installed")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("pool name is required")
	}

	destroyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(destroyCtx, "zpool", "destroy", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("zpool destroy failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// StartScrub starts a scrub on a pool.
func (m *Module) StartScrub(ctx context.Context, name string) error {
	if m == nil || m.mode == "fake" {
		return fmt.Errorf("zpool not installed")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("pool name is required")
	}
	scrubCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(scrubCtx, "zpool", "scrub", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("zpool scrub failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// StopScrub aborts an in-progress scrub.
func (m *Module) StopScrub(ctx context.Context, name string) error {
	if m == nil || m.mode == "fake" {
		return fmt.Errorf("zpool not installed")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("pool name is required")
	}
	scrubCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(scrubCtx, "zpool", "scrub", "-s", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("zpool scrub stop failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// CreateSnapshot creates a timestamped dataset snapshot.
func (m *Module) CreateSnapshot(ctx context.Context, dataset, snapshotName string) error {
	if m == nil || m.mode == "fake" {
		return fmt.Errorf("zpool not installed")
	}
	dataset = strings.TrimSpace(dataset)
	if dataset == "" {
		return fmt.Errorf("dataset is required")
	}
	snapshotName = strings.TrimSpace(snapshotName)
	if snapshotName == "" {
		snapshotName = time.Now().UTC().Format("20060102-150405")
	}
	target := dataset + "@" + snapshotName

	snapCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(snapCtx, "zfs", "snapshot", target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs snapshot failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// RecentEvents returns up to limit recent zpool event lines.
func (m *Module) RecentEvents(ctx context.Context, limit int) ([]string, error) {
	if m == nil || m.mode == "fake" {
		return nil, fmt.Errorf("zpool not installed")
	}
	if limit <= 0 {
		limit = 40
	}

	eventsCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	out, err := exec.CommandContext(eventsCtx, "zpool", "events", "-v").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("zpool events failed: %s", strings.TrimSpace(string(out)))
	}

	lines := strings.Split(string(out), "\n")
	clean := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		clean = append(clean, line)
	}
	if len(clean) > limit {
		clean = clean[len(clean)-limit:]
	}
	return clean, nil
}
