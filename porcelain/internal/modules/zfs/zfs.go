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
