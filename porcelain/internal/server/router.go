package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"sort"

	"github.com/gofiber/fiber/v3"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/auth"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/config"
	uitemplates "github.com/jasonkolodziej/porcelain/porcelain/internal/templates"
	"github.com/jasonkolodziej/porcelain/porcelain/pkg/api"
)

// NewRouter prepares the Fiber application, route set, and auth middleware.
func NewRouter(cfg config.Config, dexAuth *auth.DexAuth) (*fiber.App, error) {
	parsedTemplates, err := uitemplates.Parse()
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	app := fiber.New(fiber.Config{
		ServerHeader: "Porcelain",
		AppName:      "Porcelain",
	})

	app.Use(dexAuth.Middleware())

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

	app.Get("/", func(c fiber.Ctx) error {
		data := api.DashboardView{
			Title:               "Porcelain",
			Authentication:      authLabel(cfg.Auth),
			DBus:                dbusLabel(cfg.DBus),
			SecretsBackend:      cfg.Secrets.Backend,
			CertificatesBackend: cfg.Certificates.Backend,
			Modules:             moduleCards(),
		}

		if claims, ok := auth.ClaimsFromFiber(c); ok {
			data.Subject = claims.Subject
			data.Groups = claims.Groups
		}

		page, renderErr := renderTemplate(parsedTemplates, "base", data)
		if renderErr != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(renderErr.Error())
		}

		c.Type("html")
		return c.SendString(page)
	})

	return app, nil
}

// renderTemplate executes the requested HTML template and returns the rendered markup.
func renderTemplate(parsedTemplates *template.Template, name string, data any) (string, error) {
	var buffer bytes.Buffer
	if err := parsedTemplates.ExecuteTemplate(&buffer, name, data); err != nil {
		return "", err
	}

	return buffer.String(), nil
}

// dbusLabel returns a short summary of the runtime D-Bus integration mode.
func dbusLabel(cfg config.DBusConfig) string {
	mode := cfg.Bus
	if mode == "" {
		mode = "system"
	}

	if cfg.EnforcePeerCredentials {
		return fmt.Sprintf("%s bus with peer checks", mode)
	}

	return fmt.Sprintf("%s bus", mode)
}

// authLabel returns a short summary of whether Dex enforcement is enabled.
func authLabel(cfg config.AuthConfig) string {
	if !cfg.Enabled {
		return "disabled for scaffold"
	}

	return fmt.Sprintf("Dex via %s", cfg.IssuerURL)
}

// moduleCards returns the scaffolded feature areas shown on the dashboard.
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
