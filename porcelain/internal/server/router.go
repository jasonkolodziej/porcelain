package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/auth"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/config"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules/diagnostics"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules/storage"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/server/middleware"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/templates"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/viewdata"
	"github.com/jasonkolodziej/porcelain/porcelain/pkg/api"
)

// NewRouter prepares the Fiber application, route set, auth middleware,
// and static asset serving for the embedded UI bundle. registry may be nil
// in which case the router runs in scaffold-only mode.
func NewRouter(cfg config.Config, dexAuth *auth.DexAuth, registry *modules.Registry) (*fiber.App, error) {
	engine, err := templates.NewEngine()
	if err != nil {
		return nil, fmt.Errorf("template engine: %w", err)
	}

	app := fiber.New(fiber.Config{
		ServerHeader: "Porcelain",
		AppName:      "Porcelain",
	})

	app.Use(middleware.MTLSPeerIdentity())
	app.Use(dexAuth.Middleware())

	// Static assets are served straight from the embedded /static FS so the
	// CSS files in internal/templates/static stay editable as standalone
	// stylesheets without a separate build step.
	staticFS := engine.StaticFS()
	app.Get("/static/*", staticHandler(staticFS))

	app.Get("/healthz", func(c fiber.Ctx) error {
		return c.Status(fiber.StatusOK).SendString("ok")
	})

	app.Get("/api/modules", func(c fiber.Ctx) error {
		payload, marshalErr := json.Marshal(moduleCards())
		if marshalErr != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(marshalErr.Error())
		}
		c.Type("json")
		return c.Send(payload)
	})

	app.Get("/", renderPage(engine, cfg, registry, "dashboard", "Dashboard", func(c fiber.Ctx) any {
		return dashboardData(c.Context(), registry)
	}))

	app.Get("/storage", renderPage(engine, cfg, registry, "storage", "Storage", func(c fiber.Ctx) any {
		return storageData(c.Context(), registry)
	}))

	return app, nil
}

// dashboardData composes the dashboard payload, preferring real diagnostics
// data when the diagnostics module is registered.
func dashboardData(ctx context.Context, registry *modules.Registry) viewdata.DashboardData {
	host, _ := os.Hostname()
	if host == "" {
		host = "porcelain"
	}
	data := viewdata.Dashboard(host)

	if registry == nil {
		return data
	}
	m, ok := registry.Get("diagnostics")
	if !ok {
		return data
	}
	diag, isDiag := m.(*diagnostics.Module)
	if !isDiag {
		return data
	}
	snap, err := diag.Capture(ctx)
	if err != nil {
		return data
	}
	data.Hostname = snap.Hostname
	if snap.Uptime > 0 {
		data.Uptime = snap.Uptime.Truncate(1).String()
	}
	if len(snap.Services) > 0 {
		services := make([]viewdata.Service, 0, len(snap.Services))
		for _, s := range snap.Services {
			services = append(services, viewdata.Service{
				Name:        s.Name,
				Description: s.Description,
				Active:      s.Active,
				Status:      s.Status,
			})
			if len(services) >= 8 {
				break
			}
		}
		data.Services = services
	}
	return data
}

// storageData renders the storage page from the registered storage module,
// falling back to the empty viewdata stub when no module is registered.
func storageData(ctx context.Context, registry *modules.Registry) viewdata.StorageData {
	if registry == nil {
		return viewdata.Storage()
	}
	m, ok := registry.Get("storage")
	if !ok {
		return viewdata.Storage()
	}
	sm, isStorage := m.(*storage.Module)
	if !isStorage {
		return viewdata.Storage()
	}
	snap, _ := sm.Snapshot(ctx)
	return snap
}

// renderPage returns a Fiber handler that builds the shared template data,
// invokes the supplied data builder, and writes the rendered HTML.
//
// HTMX swaps that are not full-page boosts receive only the {{ define
// "content" }} block via RenderPartial.
func renderPage(
	engine *templates.Engine,
	cfg config.Config,
	registry *modules.Registry,
	page, title string,
	build func(c fiber.Ctx) any,
) fiber.Handler {
	return func(c fiber.Ctx) error {
		data := templates.TemplateData{
			Title:         title,
			CurrentModule: page,
			Modules:       sidebarModules(c.Context(), registry, page),
			User:          buildUserInfo(c, cfg),
			Data:          build(c),
		}

		c.Type("html")

		w := c.Response().BodyWriter()
		if isHTMXPartial(c) {
			return engine.RenderPartial(w, page, data)
		}
		return engine.Render(w, page, data)
	}
}

// isHTMXPartial reports whether the request should receive only the content
// block instead of the full base layout.
func isHTMXPartial(c fiber.Ctx) bool {
	if c.Get("HX-Request") != "true" {
		return false
	}
	if c.Get("HX-Boosted") == "true" {
		return false
	}
	return true
}

// buildUserInfo derives the user-menu identity from the verified mTLS peer
// (always present because the listener requires client certificates) and
// any Dex claims attached by the auth middleware.
func buildUserInfo(c fiber.Ctx, _ config.Config) templates.UserInfo {
	info := templates.UserInfo{Name: "anonymous", Initials: "?"}

	if identity, ok := middleware.PeerIdentityFromFiber(c); ok {
		info.Name = identity.CommonName
		info.Email = firstNonEmpty(identity.EmailAddresses)
		info.Initials = initialsFor(identity.CommonName)
	}

	if claims, ok := auth.ClaimsFromFiber(c); ok {
		if claims.Subject != "" && claims.Subject != "unknown" {
			info.Name = claims.Subject
			info.Initials = initialsFor(claims.Subject)
		}
		info.Groups = append([]string(nil), claims.Groups...)
	}

	return info
}

// firstNonEmpty returns the first non-empty entry of in, or the empty string.
func firstNonEmpty(in []string) string {
	for _, v := range in {
		if v != "" {
			return v
		}
	}
	return ""
}

// initialsFor produces a 1-2 character monogram for the user-menu avatar.
func initialsFor(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "?"
	}
	if i := strings.Index(name, "@"); i >= 0 {
		name = name[:i]
	}
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == ' ' || r == '.' || r == '_' || r == '-'
	})
	switch len(parts) {
	case 0:
		return strings.ToUpper(string([]rune(name)[:1]))
	case 1:
		runes := []rune(parts[0])
		if len(runes) == 1 {
			return strings.ToUpper(string(runes))
		}
		return strings.ToUpper(string(runes[:2]))
	default:
		return strings.ToUpper(
			string([]rune(parts[0])[:1]) +
				string([]rune(parts[len(parts)-1])[:1]),
		)
	}
}

// staticHandler serves files from the embedded static FS under /static/*.
// Path traversal attempts are rejected before the FS lookup.
func staticHandler(staticFS fs.FS) fiber.Handler {
	return func(c fiber.Ctx) error {
		rel := strings.TrimPrefix(c.Params("*"), "/")
		if rel == "" {
			return c.SendStatus(fiber.StatusNotFound)
		}

		clean := path.Clean(rel)
		if strings.HasPrefix(clean, "..") || strings.Contains(clean, "/../") {
			return c.SendStatus(fiber.StatusForbidden)
		}

		file, err := staticFS.Open(clean)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return c.SendStatus(fiber.StatusNotFound)
			}
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		stat, statErr := file.Stat()
		_ = file.Close()
		if statErr != nil || stat.IsDir() {
			return c.SendStatus(fiber.StatusNotFound)
		}

		body, err := fs.ReadFile(staticFS, clean)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}

		c.Set(fiber.HeaderContentType, contentTypeFor(clean))
		c.Set("Cache-Control", "public, max-age=300")
		return c.Send(body)
	}
}

// contentTypeFor picks a sensible MIME type for the embedded static assets.
func contentTypeFor(name string) string {
	switch {
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "application/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".woff2"):
		return "font/woff2"
	default:
		if t := http.DetectContentType([]byte{}); t != "" {
			return t
		}
		return "application/octet-stream"
	}
}

// moduleCards returns the JSON catalogue for /api/modules. The dashboard
// sidebar uses viewdata.SidebarModules instead.
func moduleCards() []api.ModuleView {
	modules := []api.ModuleView{
		{Name: "Storage", Summary: "UDisks2, ZFS, and encrypted volume workflows", Status: "planned"},
		{Name: "Network", Summary: "NetworkManager profiles, interfaces, and connectivity", Status: "planned"},
		{Name: "Firewall", Summary: "firewalld zones, services, and rich rules", Status: "planned"},
		{Name: "Podman", Summary: "Container, pod, and Quadlet orchestration", Status: "planned"},
		{Name: "Diagnostics", Summary: "Journal, rpm-ostree, and state bundle collection", Status: "planned"},
		{Name: "Sensors", Summary: "lm-sensors discovery and machine telemetry", Status: "planned"},
		{Name: "ZFS", Summary: "Pool, dataset, scrub, and snapshot management", Status: "planned"},
		{Name: "Cloudflare", Summary: "Tunnel credentials, ingress rules, and service health", Status: "planned"},
	}

	sort.Slice(modules, func(i, j int) bool {
		return modules[i].Name < modules[j].Name
	})
	return modules
}

// sidebarModules returns the navigation entries with status badges overlaid
// from the registered modules.
func sidebarModules(ctx context.Context, registry *modules.Registry, currentID string) []templates.ModuleNav {
	nav := viewdata.SidebarModules(currentID)
	if registry == nil {
		return nav
	}
	for i := range nav {
		m, ok := registry.Get(nav[i].ID)
		if !ok {
			continue
		}
		st := m.Status(ctx)
		nav[i].StatusBadge = badgeFor(st)
	}
	return nav
}

// badgeFor maps a Module Status onto the sidebar badge style.
func badgeFor(st modules.Status) *templates.StatusBadge {
	switch st.Health {
	case modules.HealthOK:
		return &templates.StatusBadge{Text: "ok", Class: "badge-success"}
	case modules.HealthDegraded:
		return &templates.StatusBadge{Text: "dev", Class: "badge-warning"}
	case modules.HealthUnavailable:
		return &templates.StatusBadge{Text: "off", Class: "badge-danger"}
	default:
		return nil
	}
}
