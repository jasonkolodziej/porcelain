package api

// ModuleView is the view model for a single dashboard capability card.
type ModuleView struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Status  string `json:"status"`
}

// DashboardView is the top-level view model rendered by the home page template.
type DashboardView struct {
	Title               string
	Subject             string
	Groups              []string
	Authentication      string
	DBus                string
	SecretsBackend      string
	CertificatesBackend string
	PeerCommonName      string
	PeerFingerprint     string
	Modules             []ModuleView
}
