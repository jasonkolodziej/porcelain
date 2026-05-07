package templates

import "html/template"

// TemplateData is the top-level structure passed to every page render.
// Field names match the references in base.html so handlers can populate
// them without indirection.
type TemplateData struct {
	Title             string
	CurrentModule     string
	Modules           []ModuleNav
	ModuleStylesheets []string
	User              UserInfo
	Flash             FlashMessage
	Data              any // Page-specific payload (e.g. DashboardData, StorageData).
}

// ModuleNav describes one entry in the persistent sidebar.
type ModuleNav struct {
	ID          string
	Name        string
	Path        string
	IconSVG     template.HTML
	StatusBadge *StatusBadge
}

// StatusBadge is the optional pill rendered next to a module name.
type StatusBadge struct {
	Text  string
	Class string
}

// UserInfo carries the authenticated subject for the top-bar user menu. The
// Dex middleware and the mTLS peer identity middleware both feed this.
type UserInfo struct {
	Name     string
	Initials string
	Email    string
	Groups   []string
}

// FlashMessage is the one-shot banner some flows set on the next render.
type FlashMessage struct {
	Type    string
	Message string
}
