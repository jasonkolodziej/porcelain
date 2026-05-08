// Package viewdata builds the per-page payloads consumed by the templates.
// It is intentionally separated from the router so future modules can swap
// the stub builders for real D-Bus / ZFS / sensors clients without touching
// HTTP code.
package viewdata

import (
	"html/template"
	"time"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/templates"
)

// SidebarModules returns the persistent navigation entries shown on every
// page. Order matches the dashboard "Modules" intent in the README.
//
// Some entries are groups (Storage, Network) whose Children render as
// collapsible sub-items in the sidebar. The status badge of a group is rolled
// up from its children by the router.
func SidebarModules(currentID string) []templates.ModuleNav {
	icon := func(d string) template.HTML {
		return template.HTML(
			`<svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="` + d + `"/></svg>`,
		)
	}
	leafIcon := func(d string) template.HTML {
		return template.HTML(
			`<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="` + d + `"/></svg>`,
		)
	}

	return []templates.ModuleNav{
		{ID: "dashboard", Name: "Dashboard", Path: "/", IconSVG: icon("M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6")},
		{
			ID:      "storage",
			Name:    "Storage",
			Path:    "/storage",
			IconSVG: icon("M5.25 14.25h13.5m-13.5 0a3 3 0 01-3-3m3 3a3 3 0 100 6h13.5a3 3 0 100-6m-16.5-3a3 3 0 013-3h13.5a3 3 0 013 3"),
			Children: []templates.ModuleNav{
				{ID: "zfs", Name: "ZFS", Path: "/storage/zfs", IconSVG: leafIcon("M20.25 6.375c0 2.278-3.694 4.125-8.25 4.125S3.75 8.653 3.75 6.375m16.5 0c0-2.278-3.694-4.125-8.25-4.125S3.75 4.097 3.75 6.375m16.5 0v11.25c0 2.278-3.694 4.125-8.25 4.125s-8.25-1.847-8.25-4.125V6.375")},
			},
		},
		{
			ID:      "network",
			Name:    "Network",
			Path:    "/network",
			IconSVG: icon("M3.055 11H5a2 2 0 012 2v1a2 2 0 002 2 2 2 0 012 2v2.945M8 3.935V5.5A2.5 2.5 0 0010.5 8h.5a2 2 0 012 2 2 2 0 104 0 2 2 0 012-2h1.064M15 20.488V18a2 2 0 012-2h3.064"),
			Children: []templates.ModuleNav{
				{ID: "firewall", Name: "Firewall", Path: "/network/firewall", IconSVG: leafIcon("M9 12.75L11.25 15 15 9.75M21 12c0 1.268-.63 2.39-1.593 3.068a3.745 3.745 0 01-1.043 3.296 3.745 3.745 0 01-3.296 1.043A3.745 3.745 0 0112 21c-1.268 0-2.39-.63-3.068-1.593a3.746 3.746 0 01-3.296-1.043 3.745 3.745 0 01-1.043-3.296A3.745 3.745 0 013 12c0-1.268.63-2.39 1.593-3.068a3.745 3.745 0 011.043-3.296 3.746 3.746 0 013.296-1.043A3.746 3.746 0 0112 3c1.268 0 2.39.63 3.068 1.593a3.746 3.746 0 013.296 1.043 3.746 3.746 0 011.043 3.296A3.745 3.745 0 0121 12z")},
				{ID: "cloudflare", Name: "Cloudflare", Path: "/network/cloudflare", IconSVG: leafIcon("M2.25 18.75a60.07 60.07 0 0115.797 2.101c.727.198 1.453-.342 1.453-1.096V18.75M3.75 4.5v.75A.75.75 0 013 6h-.75m0 0v-.375c0-.621.504-1.125 1.125-1.125H20.25M2.25 6v9m18-10.5v.75c0 .414.336.75.75.75h.75m-1.5-1.5h.375c.621 0 1.125.504 1.125 1.125v9.75c0 .621-.504 1.125-1.125 1.125h-.375m1.5-1.5H21a.75.75 0 00-.75.75v.75m0 0H3.75m0 0h-.375a1.125 1.125 0 01-1.125-1.125V15m1.5 1.5v-.75A.75.75 0 003 15h-.75M15 10.5a3 3 0 11-6 0 3 3 0 016 0z")},
			},
		},
		{ID: "podman", Name: "Containers", Path: "/podman", IconSVG: icon("M21 7.5l-9-5.25L3 7.5m18 0l-9 5.25m9-5.25v9l-9 5.25M3 7.5l9 5.25M3 7.5v9l9 5.25m0-9v9")},
		{ID: "diagnostics", Name: "Diagnostics", Path: "/diagnostics", IconSVG: icon("M3 13.125C3 12.504 3.504 12 4.125 12h2.25c.621 0 1.125.504 1.125 1.125v6.75C7.5 20.496 6.996 21 6.375 21h-2.25A1.125 1.125 0 013 19.875v-6.75zM9.75 8.625c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125v11.25c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V8.625zM16.5 4.125c0-.621.504-1.125 1.125-1.125h2.25C20.496 3 21 3.504 21 4.125v15.75c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V4.125z")},
		{ID: "sensors", Name: "Sensors", Path: "/sensors", IconSVG: icon("M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126z")},
	}
}

// DashboardData is the payload for dashboard.html. Each field maps directly
// to a {{ range .Data.X }} or {{ .Data.X }} call in the template.
type DashboardData struct {
	Hostname     string
	Uptime       string
	Stats        []Stat
	Services     []Service
	Logs         []LogLine
	QuickActions []QuickAction
	Resources    []Resource
	Sensors      []Sensor
}

// Stat is one card in the top stats row.
type Stat struct {
	Label    string
	Value    string
	Icon     template.HTML
	Change   *StatChange
	Progress *StatProgress
}

// StatChange annotates a stat with a positive/negative delta.
type StatChange struct {
	Text     string
	Positive bool
}

// StatProgress is a used/total progress bar inside a Stat card.
type StatProgress struct {
	Used  int64
	Total int64
}

// Service describes a systemd-managed service shown in the dashboard list.
type Service struct {
	Name        string
	Description string
	Active      bool
	Status      string
}

// LogLine is one entry in the Recent Events terminal-style block.
type LogLine struct {
	Timestamp string
	Level     string
	Message   string
}

// QuickAction is a clickable card in the Quick Actions grid.
type QuickAction struct {
	Label          string
	Icon           string
	Endpoint       string
	Confirm        string
	SuccessMessage string
}

// Resource is one row of the Resource Usage block.
type Resource struct {
	Name       string
	Value      string
	Percentage int
	Color      string
}

// Sensor is a single lm-sensors readout shown on the dashboard.
type Sensor struct {
	Name     string
	Value    string
	Unit     string
	Critical bool
}

// Dashboard returns a stub dashboard payload. Real D-Bus / sysfs / sensors
// integrations replace this in Phase 3.
func Dashboard(hostname string) DashboardData {
	return DashboardData{
		Hostname: hostname,
		Uptime:   "uptime stub",
		Stats: []Stat{
			{Label: "CPU", Value: "12%", Progress: &StatProgress{Used: 12, Total: 100}},
			{Label: "Memory", Value: "3.4 GiB", Progress: &StatProgress{Used: 3 * 1024 * 1024 * 1024, Total: 16 * 1024 * 1024 * 1024}},
			{Label: "Containers", Value: "0 running", Change: &StatChange{Text: "scaffold", Positive: true}},
			{Label: "Pools", Value: "0 healthy", Change: &StatChange{Text: "scaffold", Positive: true}},
		},
		Services: []Service{
			{Name: "porcelain.service", Description: "Porcelain control plane", Active: true, Status: "active"},
		},
		Logs: []LogLine{
			{Timestamp: time.Now().Format("15:04:05"), Level: "INF", Message: "porcelain bootstrapped (scaffold mode)"},
		},
		QuickActions: []QuickAction{
			{Label: "Reboot", Icon: "↻", Endpoint: "/api/host/reboot", Confirm: "Reboot host?", SuccessMessage: "Reboot scheduled"},
			{Label: "Update", Icon: "⬇", Endpoint: "/api/host/update", Confirm: "Pull updates?", SuccessMessage: "Update started"},
		},
		Resources: []Resource{
			{Name: "Disk", Value: "—", Percentage: 0, Color: "bg-accent-cyan"},
			{Name: "Network", Value: "—", Percentage: 0, Color: "bg-accent-emerald"},
		},
		Sensors: []Sensor{
			{Name: "Package", Value: "—", Unit: "°C"},
		},
	}
}

// StorageData backs storage.html.
type StorageData struct {
	Pools            []Pool
	BlockDevices     []BlockDevice
	AvailableDevices []BlockDevice
}

// Pool maps onto the ZFS pool card grid.
type Pool struct {
	Name             string
	Health           string
	RaidType         string
	Allocated        int64
	Size             int64
	CompressionRatio string
	Deduplication    string
	ScrubStatus      string
	LastScrub        string
	Datasets         []Dataset
}

// Dataset is a single child dataset row inside a Pool card.
type Dataset struct {
	Name      string
	Used      int64
	Available int64
}

// BlockDevice is one row of the block device table.
type BlockDevice struct {
	ObjectPath     string
	Device         string
	Path           string
	Model          string
	Size           int64
	Type           string
	Filesystem     string
	Label          string
	MountPoints    []string
	IsEncrypted    bool
	EncryptionType string
	IsBootDevice   bool
}

// Storage returns a stub storage payload.
func Storage() StorageData {
	return StorageData{
		Pools:            []Pool{},
		BlockDevices:     []BlockDevice{},
		AvailableDevices: []BlockDevice{},
	}
}

// PlaceholderData backs the generic "coming soon" page used by group
// landing pages and module sub-pages whose full UI hasn't shipped yet.
type PlaceholderData struct {
	Heading     string
	Blurb       string
	Detail      string
	StatusBadge *templates.StatusBadge
	Children    []templates.ModuleNav
}

// DiagnosticsPageData backs modules/diagnostics.html.
type DiagnosticsPageData struct {
	Heading         string
	Blurb           string
	Detail          string
	Hostname        string
	Kernel          string
	OS              string
	Uptime          string
	RunningServices int
	TotalServices   int
	Services        []DiagnosticsService
	JournalTail     []string
	OSTreeStatus    string
	Modules         []DiagnosticsModuleStatus
	StatusBadge     *templates.StatusBadge
	BundlePath      string
	BundleButton    string
}

// DiagnosticsService is one service row in the diagnostics page.
type DiagnosticsService struct {
	Name        string
	Description string
	Status      string
	Active      bool
}

// DiagnosticsModuleStatus is one module health row in the diagnostics page.
type DiagnosticsModuleStatus struct {
	ID     string
	Name   string
	Health string
	Detail string
}

// PodmanPageData backs modules/podman.html.
type PodmanPageData struct {
	Heading     string
	Blurb       string
	Detail      string
	Containers  []PodmanContainer
	Images      []PodmanImage
	StatusBadge *templates.StatusBadge
}

// PodmanContainer is one container row in the podman page.
type PodmanContainer struct {
	ID     string
	Name   string
	Image  string
	State  string
	Status string
}

// PodmanImage is one image row in the podman page.
type PodmanImage struct {
	Repository string
	Tag        string
	ID         string
	Size       string
}

// SensorsPageData backs modules/sensors.html.
type SensorsPageData struct {
	Heading     string
	Blurb       string
	Detail      string
	Chips       []SensorChip
	StatusBadge *templates.StatusBadge
}

// SensorChip is one chip/device section in the sensors page.
type SensorChip struct {
	Name     string
	Readings []SensorReading
}

// SensorReading is one parsed reading from lm-sensors.
type SensorReading struct {
	Name      string
	Value     string
	Unit      string
	Critical  bool
	Threshold string
	High      string
}

// NetworkPageData backs modules/network.html.
type NetworkPageData struct {
	Heading     string
	Blurb       string
	Detail      string
	StatusBadge *templates.StatusBadge
	Enabled     bool
	Devices     []NetworkDevice
}

// NetworkDevice is one NetworkManager interface row.
type NetworkDevice struct {
	Path      string
	Interface string
	Type      string
	State     string
	MAC       string
}

// FirewallPageData backs modules/firewall.html.
type FirewallPageData struct {
	Heading          string
	Blurb            string
	Detail           string
	StatusBadge      *templates.StatusBadge
	DefaultZone      string
	Zones            []FirewallZone
	AllKnownServices []string
}

// FirewallZone is one firewalld zone card.
type FirewallZone struct {
	Name       string
	Default    bool
	Interfaces []string
	Services   []string
}

// ZFSPageData backs modules/zfs.html (Phase 4).
type ZFSPageData struct {
	Heading     string
	Blurb       string
	Detail      string
	Pools       []ZFSPool
	StatusBadge *templates.StatusBadge
}

// ZFSPool is one zpool entry.
type ZFSPool struct {
	Name             string
	Health           string
	Allocated        int64
	Size             int64
	CompressionRatio string
	Deduplication    string
	ScrubStatus      string
	LastScrub        string
	Datasets         []ZFSDataset
}

// ZFSDataset is one zfs dataset under a pool.
type ZFSDataset struct {
	Name      string
	Used      int64
	Available int64
}

// CloudflarePageData backs modules/cloudflare.html (Phase 4).
type CloudflarePageData struct {
	Heading     string
	Blurb       string
	Detail      string
	Version     string
	Tunnels     []CloudflareTunnel
	StatusBadge *templates.StatusBadge
}

// CloudflareTunnel is one cloudflared tunnel entry.
type CloudflareTunnel struct {
	ID        string
	Name      string
	CreatedAt string
	Status    string
}
