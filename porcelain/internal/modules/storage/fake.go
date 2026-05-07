package storage

import (
	"context"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/viewdata"
)

// fakeBackend is the developer-only backend used when no D-Bus connection
// is reachable (typically macOS workstations). It returns enough realistic
// data to exercise the storage page templates and HTMX swaps without a
// running daemon on the host.
type fakeBackend struct {
	// note is a free-form description of why the fake was selected. It is
	// surfaced in module Status when non-empty.
	note string
}

// NewFakeBackend returns a Backend suitable for local development.
func NewFakeBackend() *fakeBackend {
	return &fakeBackend{}
}

// Kind implements Backend.
func (b *fakeBackend) Kind() string { return "fake" }

// Close implements Backend.
func (b *fakeBackend) Close() error { return nil }

// Snapshot returns a representative storage layout.
func (b *fakeBackend) Snapshot(_ context.Context) (viewdata.StorageData, error) {
	const TiB = int64(1024) * 1024 * 1024 * 1024
	return viewdata.StorageData{
		Pools: []viewdata.Pool{
			{
				Name:             "tank",
				Health:           "ONLINE",
				RaidType:         "raidz2",
				Allocated:        2 * TiB,
				Size:             8 * TiB,
				CompressionRatio: "1.34",
				Deduplication:    "off",
				ScrubStatus:      "idle",
				LastScrub:        "3 days ago",
				Datasets: []viewdata.Dataset{
					{Name: "tank/home", Used: 800 * 1024 * 1024 * 1024, Available: 4 * TiB},
					{Name: "tank/podman", Used: 120 * 1024 * 1024 * 1024, Available: 4 * TiB},
				},
			},
		},
		BlockDevices: []viewdata.BlockDevice{
			{Device: "/dev/sda", Model: "Samsung SSD 870 EVO", Size: 2 * TiB, Type: "filesystem", Filesystem: "zfs_member"},
			{Device: "/dev/sdb", Model: "WD Red Pro", Size: 4 * TiB, Type: "filesystem", Filesystem: "zfs_member"},
			{Device: "/dev/sdc", Model: "Crucial MX500", Size: 1 * TiB, Type: "encrypted", IsEncrypted: true, EncryptionType: "LUKS"},
		},
		AvailableDevices: []viewdata.BlockDevice{
			{Device: "/dev/sdd", Path: "/dev/sdd", Model: "Seagate IronWolf", Size: 4 * TiB},
		},
	}, nil
}
