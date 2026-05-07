package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/auth"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/config"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules/cloudflare"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules/diagnostics"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules/firewall"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules/network"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules/podman"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules/sensors"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules/storage"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules/zfs"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/policy"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/server/middleware"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/templates"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/viewdata"
	"github.com/jasonkolodziej/porcelain/porcelain/pkg/api"
)

const errModuleRegistryUnavailable = "module registry unavailable"

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
	authorizer := policy.NewOPAStyleAuthorizer(cfg.Auth.AdminGroup, cfg.Auth.Enabled)

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

	registerAPIRoutes(app, registry, authorizer)
	registerPageRoutes(app, engine, cfg, registry)

	return app, nil
}

func registerAPIRoutes(app *fiber.App, registry *modules.Registry, authorizer policy.Authorizer) {
	registerDiagnosticsBundleAPI(app, registry)
	registerPodmanAPI(app, registry, authorizer)
	registerSystemdAPI(app, registry, authorizer)
	registerNetworkAPI(app, registry, authorizer)
	registerFirewallAPI(app, registry, authorizer)
	registerStorageAPI(app, registry, authorizer)
	registerZFSAPI(app, registry, authorizer)
	registerCloudflareAPI(app, registry, authorizer)
	registerSensorsAPI(app, registry, authorizer)
}

func registerDiagnosticsBundleAPI(app *fiber.App, registry *modules.Registry) {
	app.Get("/api/diagnostics/bundle", func(c fiber.Ctx) error {
		bundle, filename, bundleErr := diagnosticsBundle(c.Context(), registry)
		if bundleErr != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(bundleErr.Error())
		}
		c.Set(fiber.HeaderContentType, "application/gzip")
		c.Set(fiber.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%q", filename))
		return c.Send(bundle)
	})
}

func registerPodmanAPI(app *fiber.App, registry *modules.Registry, authorizer policy.Authorizer) {
	app.Post("/api/podman/containers/:id/:action", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "podman.container."+c.Params("action"), c.Params("id")); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("podman")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("podman module not registered")
		}
		pm, isPodman := m.(*podman.Module)
		if !isPodman {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid podman module type")
		}
		if err := pm.ContainerAction(c.Context(), c.Params("id"), c.Params("action")); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(map[string]string{"status": "ok"})
	})

	// GET /api/podman/containers/:id/logs?tail=N
	// Returns escaped HTML for use by the HTMX log drawer.
	app.Get("/api/podman/containers/:id/logs", func(c fiber.Ctx) error {
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("podman")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("podman module not registered")
		}
		pm, isPodman := m.(*podman.Module)
		if !isPodman {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid podman module type")
		}

		tail := 100
		if raw := c.Query("tail"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 2000 {
				tail = n
			}
		}

		logs, err := pm.ContainerLogs(c.Context(), c.Params("id"), tail)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		c.Type("html")
		return c.SendString("<pre class=\"m-0 whitespace-pre-wrap font-mono text-xs leading-relaxed\">" + html.EscapeString(logs) + "</pre>")
	})

	app.Get("/api/podman/containers/:id/inspect", func(c fiber.Ctx) error {
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("podman")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("podman module not registered")
		}
		pm, isPodman := m.(*podman.Module)
		if !isPodman {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid podman module type")
		}
		body, err := pm.InspectContainer(c.Context(), c.Params("id"))
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		c.Type("html")
		return c.SendString("<pre class=\"m-0 whitespace-pre-wrap font-mono text-xs leading-relaxed\">" + html.EscapeString(body) + "</pre>")
	})

	app.Get("/api/podman/images/:ref/inspect", func(c fiber.Ctx) error {
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("podman")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("podman module not registered")
		}
		pm, isPodman := m.(*podman.Module)
		if !isPodman {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid podman module type")
		}
		body, err := pm.InspectImage(c.Context(), c.Params("ref"))
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		c.Type("html")
		return c.SendString("<pre class=\"m-0 whitespace-pre-wrap font-mono text-xs leading-relaxed\">" + html.EscapeString(body) + "</pre>")
	})

	app.Post("/api/podman/images/pull", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "podman.image.pull", "global"); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("podman")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("podman module not registered")
		}
		pm, isPodman := m.(*podman.Module)
		if !isPodman {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid podman module type")
		}
		image := strings.TrimSpace(c.FormValue("image"))
		out, err := pm.PullImage(c.Context(), image)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		c.Type("html")
		return c.SendString("<pre class=\"m-0 whitespace-pre-wrap font-mono text-xs leading-relaxed\">" + html.EscapeString(out) + "</pre>")
	})

	app.Post("/api/podman/images/build", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "podman.image.build", "global"); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("podman")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("podman module not registered")
		}
		pm, isPodman := m.(*podman.Module)
		if !isPodman {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid podman module type")
		}
		out, err := pm.BuildImage(
			c.Context(),
			strings.TrimSpace(c.FormValue("contextPath")),
			strings.TrimSpace(c.FormValue("dockerfile")),
			strings.TrimSpace(c.FormValue("tag")),
		)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		c.Type("html")
		return c.SendString("<pre class=\"m-0 whitespace-pre-wrap font-mono text-xs leading-relaxed\">" + html.EscapeString(out) + "</pre>")
	})

	app.Post("/api/podman/containers/:id/exec", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "podman.container.exec", c.Params("id")); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("podman")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("podman module not registered")
		}
		pm, isPodman := m.(*podman.Module)
		if !isPodman {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid podman module type")
		}
		cmdLine := strings.TrimSpace(c.FormValue("command"))
		parts := strings.Fields(cmdLine)
		out, err := pm.Exec(c.Context(), c.Params("id"), parts)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		c.Type("html")
		return c.SendString("<pre class=\"m-0 whitespace-pre-wrap font-mono text-xs leading-relaxed\">" + html.EscapeString(out) + "</pre>")
	})

	app.Post("/api/podman/registry/login", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "podman.registry.login", "global"); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("podman")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("podman module not registered")
		}
		pm, isPodman := m.(*podman.Module)
		if !isPodman {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid podman module type")
		}
		err := pm.RegistryLogin(c.Context(), c.FormValue("registry"), c.FormValue("username"), c.FormValue("password"))
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(map[string]string{"status": "ok"})
	})

	app.Post("/api/podman/registry/logout", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "podman.registry.logout", "global"); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("podman")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("podman module not registered")
		}
		pm, isPodman := m.(*podman.Module)
		if !isPodman {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid podman module type")
		}
		err := pm.RegistryLogout(c.Context(), c.FormValue("registry"))
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(map[string]string{"status": "ok"})
	})
}

func registerStorageAPI(app *fiber.App, registry *modules.Registry, authorizer policy.Authorizer) {
	app.Post("/api/storage/devices/format", func(c fiber.Ctx) error {
		device := strings.TrimSpace(c.FormValue("device"))
		if err := authorizeWrite(c, authorizer, "storage.device.format", device); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("storage")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("storage module not registered")
		}
		sm, isStorage := m.(*storage.Module)
		if !isStorage {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid storage module type")
		}
		if !strings.HasPrefix(device, "/") {
			device = "/" + device
		}
		if err := sm.FormatDevice(c.Context(), device, c.FormValue("fsType")); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(map[string]string{"status": "ok"})
	})

	app.Post("/api/storage/devices/unlock", func(c fiber.Ctx) error {
		device := strings.TrimSpace(c.FormValue("device"))
		if err := authorizeWrite(c, authorizer, "storage.device.unlock", device); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("storage")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("storage module not registered")
		}
		sm, isStorage := m.(*storage.Module)
		if !isStorage {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid storage module type")
		}
		if !strings.HasPrefix(device, "/") {
			device = "/" + device
		}
		if err := sm.UnlockDevice(c.Context(), device); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(map[string]string{"status": "ok"})
	})
}

func registerZFSAPI(app *fiber.App, registry *modules.Registry, authorizer policy.Authorizer) {
	app.Post("/api/zfs/pools", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "zfs.pool.create", "global"); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("zfs")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("zfs module not registered")
		}
		zm, isZFS := m.(*zfs.Module)
		if !isZFS {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid zfs module type")
		}
		var devices []string
		if mf, err := c.MultipartForm(); err == nil && mf != nil {
			devices = append(devices, mf.Value["devices"]...)
		}
		if err := zm.CreatePool(c.Context(), c.FormValue("name"), c.FormValue("raid"), c.FormValue("compression"), devices); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(map[string]string{"status": "ok"})
	})

	app.Post("/api/zfs/pools/:name/scrub", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "zfs.pool.scrub", c.Params("name")); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("zfs")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("zfs module not registered")
		}
		zm, isZFS := m.(*zfs.Module)
		if !isZFS {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid zfs module type")
		}
		if err := zm.StartScrub(c.Context(), c.Params("name")); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(map[string]string{"status": "ok"})
	})

	app.Post("/api/zfs/pools/:name/scrub/stop", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "zfs.pool.scrub.stop", c.Params("name")); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("zfs")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("zfs module not registered")
		}
		zm, isZFS := m.(*zfs.Module)
		if !isZFS {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid zfs module type")
		}
		if err := zm.StopScrub(c.Context(), c.Params("name")); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(map[string]string{"status": "ok"})
	})

	app.Post("/api/zfs/pools/:name/snapshot", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "zfs.pool.snapshot", c.Params("name")); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("zfs")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("zfs module not registered")
		}
		zm, isZFS := m.(*zfs.Module)
		if !isZFS {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid zfs module type")
		}
		if err := zm.CreateSnapshot(c.Context(), c.Params("name"), c.FormValue("snapshotName")); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(map[string]string{"status": "ok"})
	})

	app.Delete("/api/zfs/pools/:name", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "zfs.pool.destroy", c.Params("name")); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("zfs")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("zfs module not registered")
		}
		zm, isZFS := m.(*zfs.Module)
		if !isZFS {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid zfs module type")
		}
		if err := zm.DestroyPool(c.Context(), c.Params("name")); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(map[string]string{"status": "ok"})
	})

	app.Get("/api/zfs/events", func(c fiber.Ctx) error {
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("zfs")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("zfs module not registered")
		}
		zm, isZFS := m.(*zfs.Module)
		if !isZFS {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid zfs module type")
		}
		limit := 40
		if raw := c.Query("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 200 {
				limit = n
			}
		}
		lines, err := zm.RecentEvents(c.Context(), limit)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		c.Type("html")
		return c.SendString("<pre class=\"m-0 whitespace-pre-wrap font-mono text-xs leading-relaxed\">" + html.EscapeString(strings.Join(lines, "\n")) + "</pre>")
	})
}

func registerCloudflareAPI(app *fiber.App, registry *modules.Registry, authorizer policy.Authorizer) {
	app.Post("/api/cloudflare/provision", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "cloudflare.tunnel.provision", "global"); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("cloudflare")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("cloudflare module not registered")
		}
		cm, isCloudflare := m.(*cloudflare.Module)
		if !isCloudflare {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid cloudflare module type")
		}
		err := cm.ProvisionWithToken(
			c.Context(),
			c.FormValue("name"),
			c.FormValue("token"),
			c.FormValue("hostname"),
			c.FormValue("serviceURL"),
			c.FormValue("image"),
		)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(map[string]string{"status": "ok"})
	})

	app.Get("/api/cloudflare/tunnels/:name/logs", func(c fiber.Ctx) error {
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("cloudflare")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("cloudflare module not registered")
		}
		cm, isCloudflare := m.(*cloudflare.Module)
		if !isCloudflare {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid cloudflare module type")
		}
		tail := 120
		if raw := c.Query("tail"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 5000 {
				tail = n
			}
		}
		logs, err := cm.TunnelContainerLogs(c.Context(), c.Params("name"), tail)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		c.Type("html")
		return c.SendString("<pre class=\"m-0 whitespace-pre-wrap font-mono text-xs leading-relaxed\">" + html.EscapeString(logs) + "</pre>")
	})

	app.Post("/api/cloudflare/tunnels/:name/restart", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "cloudflare.tunnel.restart", c.Params("name")); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("cloudflare")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("cloudflare module not registered")
		}
		cm, isCloudflare := m.(*cloudflare.Module)
		if !isCloudflare {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid cloudflare module type")
		}
		if err := cm.RestartTunnelContainer(c.Context(), c.Params("name")); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(map[string]string{"status": "ok"})
	})
}

func registerSensorsAPI(app *fiber.App, registry *modules.Registry, authorizer policy.Authorizer) {
	app.Post("/api/sensors/thresholds", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "sensors.threshold.set", "global"); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("sensors")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("sensors module not registered")
		}
		sm, isSensors := m.(*sensors.Module)
		if !isSensors {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid sensors module type")
		}
		if err := sm.SetThreshold(c.FormValue("chip"), c.FormValue("reading"), c.FormValue("threshold")); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(map[string]string{"status": "ok"})
	})

	app.Delete("/api/sensors/thresholds", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "sensors.threshold.clear", "global"); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("sensors")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("sensors module not registered")
		}
		sm, isSensors := m.(*sensors.Module)
		if !isSensors {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid sensors module type")
		}
		sm.ClearThreshold(c.Query("chip"), c.Query("reading"))
		return c.JSON(map[string]string{"status": "ok"})
	})

	app.Post("/api/sensors/thresholds/clear", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "sensors.threshold.clear", "global"); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("sensors")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("sensors module not registered")
		}
		sm, isSensors := m.(*sensors.Module)
		if !isSensors {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid sensors module type")
		}
		sm.ClearThreshold(c.FormValue("chip"), c.FormValue("reading"))
		return c.JSON(map[string]string{"status": "ok"})
	})

	app.Get("/api/sensors/history", func(c fiber.Ctx) error {
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("sensors")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("sensors module not registered")
		}
		sm, isSensors := m.(*sensors.Module)
		if !isSensors {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid sensors module type")
		}
		limit := 30
		if raw := c.Query("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 240 {
				limit = n
			}
		}
		points := sm.History(c.Query("chip"), c.Query("reading"), limit)
		if len(points) == 0 {
			return c.SendString("No history captured yet for this reading.")
		}
		lines := make([]string, 0, len(points))
		for _, p := range points {
			lines = append(lines, p.Timestamp+"  "+fmt.Sprintf("%.2f", p.Value))
		}
		c.Type("html")
		return c.SendString("<pre class=\"m-0 whitespace-pre-wrap font-mono text-xs leading-relaxed\">" + html.EscapeString(strings.Join(lines, "\n")) + "</pre>")
	})
}

func registerSystemdAPI(app *fiber.App, registry *modules.Registry, authorizer policy.Authorizer) {
	app.Post("/api/systemd/units/:name/:action", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "systemd.unit."+c.Params("action"), c.Params("name")); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("diagnostics")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("diagnostics module not registered")
		}
		dm, ok := m.(*diagnostics.Module)
		if !ok {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid diagnostics module type")
		}
		if err := dm.UnitAction(c.Context(), c.Params("name"), c.Params("action")); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(map[string]string{"status": "ok"})
	})
}

func registerNetworkAPI(app *fiber.App, registry *modules.Registry, authorizer policy.Authorizer) {
	app.Post("/api/network/enabled/:state", func(c fiber.Ctx) error {
		if err := authorizeWrite(c, authorizer, "network.global.enable", c.Params("state")); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("network")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("network module not registered")
		}
		nm, ok := m.(*network.Module)
		if !ok {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid network module type")
		}
		enabled := strings.EqualFold(c.Params("state"), "true") || strings.EqualFold(c.Params("state"), "on")
		if err := nm.SetEnabled(c.Context(), enabled); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(map[string]string{"status": "ok"})
	})
}

func registerFirewallAPI(app *fiber.App, registry *modules.Registry, authorizer policy.Authorizer) {
	app.Post("/api/firewall/zones/:zone/services/:service/:state", func(c fiber.Ctx) error {
		zone := c.Params("zone")
		service := c.Params("service")
		state := c.Params("state")
		if err := authorizeWrite(c, authorizer, "firewall.zone.service."+state, zone+"/"+service); err != nil {
			return err
		}
		if registry == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(errModuleRegistryUnavailable)
		}
		m, ok := registry.Get("firewall")
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).SendString("firewall module not registered")
		}
		fm, ok := m.(*firewall.Module)
		if !ok {
			return c.Status(fiber.StatusInternalServerError).SendString("invalid firewall module type")
		}
		enabled := strings.EqualFold(state, "enable") || strings.EqualFold(state, "on") || strings.EqualFold(state, "true")
		if err := fm.SetService(c.Context(), zone, service, enabled); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(map[string]string{"status": "ok"})
	})
}

func registerPageRoutes(app *fiber.App, engine *templates.Engine, cfg config.Config, registry *modules.Registry) {
	app.Get("/", renderPage(engine, cfg, registry, "dashboard", "dashboard", "Dashboard", func(c fiber.Ctx) any {
		return dashboardData(c.Context(), registry)
	}))

	app.Get("/storage", renderPage(engine, cfg, registry, "storage", "storage", "Storage", func(c fiber.Ctx) any {
		return storageData(c.Context(), registry)
	}))

	app.Get("/storage/zfs", renderPage(engine, cfg, registry, "storage-zfs", "zfs", "ZFS", func(c fiber.Ctx) any {
		return zfsPageData(c.Context(), registry)
	}))

	app.Get("/network", renderPage(engine, cfg, registry, "network", "network", "Network", func(c fiber.Ctx) any {
		return networkPageData(c.Context(), registry)
	}))

	app.Get("/network/firewall", renderPage(engine, cfg, registry, "network-firewall", "firewall", "Firewall", func(c fiber.Ctx) any {
		return firewallPageData(c.Context(), registry)
	}))

	app.Get("/network/cloudflare", renderPage(engine, cfg, registry, "network-cloudflare", "cloudflare", "Cloudflare", func(c fiber.Ctx) any {
		return cloudflarePageData(c.Context(), registry)
	}))

	app.Get("/podman", renderPage(engine, cfg, registry, "podman", "podman", "Containers", func(c fiber.Ctx) any {
		return podmanPageData(c.Context(), registry)
	}))

	app.Get("/diagnostics", renderPage(engine, cfg, registry, "diagnostics", "diagnostics", "Diagnostics", func(c fiber.Ctx) any {
		return diagnosticsPageData(c.Context(), registry)
	}))

	app.Get("/sensors", renderPage(engine, cfg, registry, "sensors", "sensors", "Sensors", func(c fiber.Ctx) any {
		return sensorsPageData(c.Context(), registry)
	}))
}

func authorizeWrite(c fiber.Ctx, a policy.Authorizer, action, resource string) error {
	claims, _ := auth.ClaimsFromFiber(c)
	peer, _ := middleware.PeerIdentityFromFiber(c)
	decision, err := a.Authorize(c.Context(), policy.Input{
		Subject:   claims.Subject,
		Groups:    claims.Groups,
		Action:    "write." + action,
		Resource:  resource,
		PeerCN:    peer.CommonName,
		PeerFP:    peer.FingerprintSHA256,
		AuthToken: claims.Token,
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}
	if !decision.Allowed {
		return c.Status(fiber.StatusForbidden).SendString("policy denied: " + decision.Reason)
	}
	return nil
}

// placeholderFor builds a generic PlaceholderData payload for a module-level
// page that has not yet shipped its full UI. The status badge is taken from
// the registry when the named module is registered.
func placeholderFor(ctx context.Context, registry *modules.Registry, id, heading, blurb, detail string) viewdata.PlaceholderData {
	data := viewdata.PlaceholderData{Heading: heading, Blurb: blurb, Detail: detail}
	if registry == nil {
		return data
	}
	if m, ok := registry.Get(id); ok {
		data.StatusBadge = badgeFor(m.Status(ctx))
	}
	return data
}

// networkOverview composes the Network landing page from the Firewall and
// Cloudflare sub-modules so the user sees both at a glance.
func networkOverview(ctx context.Context, registry *modules.Registry) viewdata.PlaceholderData {
	data := viewdata.PlaceholderData{
		Heading: "Network",
		Blurb:   "Interface, firewall, and tunnel summary",
		Detail:  "Pick a sub-page below to drill into a specific networking concern.",
	}
	for _, child := range []struct {
		id, name, path string
	}{
		{"firewall", "Firewall", "/network/firewall"},
		{"cloudflare", "Cloudflare", "/network/cloudflare"},
	} {
		entry := templates.ModuleNav{ID: child.id, Name: child.name, Path: child.path}
		if registry != nil {
			if m, ok := registry.Get(child.id); ok {
				entry.StatusBadge = badgeFor(m.Status(ctx))
			}
		}
		data.Children = append(data.Children, entry)
	}
	if registry != nil {
		if m, ok := registry.Get("network"); ok {
			data.StatusBadge = badgeFor(m.Status(ctx))
		}
	}
	return data
}

func networkPageData(ctx context.Context, registry *modules.Registry) viewdata.NetworkPageData {
	data := viewdata.NetworkPageData{
		Heading: "Network",
		Blurb:   "Interface and connectivity controls via NetworkManager",
		Detail:  "NetworkManager module unavailable",
	}
	if registry == nil {
		return data
	}

	m, ok := registry.Get("network")
	if !ok {
		return data
	}
	data.StatusBadge = badgeFor(m.Status(ctx))

	nm, ok := m.(*network.Module)
	if !ok {
		data.Detail = "invalid network module type"
		return data
	}

	devices, err := nm.Devices(ctx)
	if err != nil {
		data.Detail = err.Error()
		return data
	}

	data.Enabled = true
	data.Detail = fmt.Sprintf("%d interfaces detected", len(devices))
	data.Devices = make([]viewdata.NetworkDevice, 0, len(devices))
	for _, d := range devices {
		data.Devices = append(data.Devices, viewdata.NetworkDevice{
			Path:      string(d.Path),
			Interface: d.Interface,
			Type:      networkDeviceTypeLabel(d.DeviceType),
			State:     networkDeviceStateLabel(d.State),
			MAC:       d.HwAddress,
		})
		if d.State <= 20 {
			data.Enabled = false
		}
	}
	return data
}

func firewallPageData(ctx context.Context, registry *modules.Registry) viewdata.FirewallPageData {
	data := viewdata.FirewallPageData{
		Heading: "Firewall",
		Blurb:   "Zone and service controls via firewalld",
		Detail:  "firewalld module unavailable",
	}
	if registry == nil {
		return data
	}

	m, ok := registry.Get("firewall")
	if !ok {
		return data
	}
	data.StatusBadge = badgeFor(m.Status(ctx))

	fm, ok := m.(*firewall.Module)
	if !ok {
		data.Detail = "invalid firewall module type"
		return data
	}

	summary, err := fm.Summary(ctx)
	if err != nil {
		data.Detail = err.Error()
		return data
	}

	data.DefaultZone = summary.Default
	data.Zones = make([]viewdata.FirewallZone, 0, len(summary.Zones))
	knownServices := map[string]struct{}{}
	for _, z := range summary.Zones {
		services := append([]string(nil), summary.ServicesByZone[z]...)
		sort.Strings(services)
		for _, svc := range services {
			knownServices[svc] = struct{}{}
		}
		ifaces := append([]string(nil), summary.InterfacesByZone[z]...)
		sort.Strings(ifaces)
		data.Zones = append(data.Zones, viewdata.FirewallZone{
			Name:       z,
			Default:    z == summary.Default,
			Interfaces: ifaces,
			Services:   services,
		})
	}

	for svc := range knownServices {
		data.AllKnownServices = append(data.AllKnownServices, svc)
	}
	sort.Strings(data.AllKnownServices)
	data.Detail = fmt.Sprintf("%d zones loaded (default: %s)", len(data.Zones), summary.Default)
	return data
}

func networkDeviceTypeLabel(t uint32) string {
	switch t {
	case 1:
		return "ethernet"
	case 2:
		return "wifi"
	case 5:
		return "bridge"
	case 6:
		return "tun"
	case 8:
		return "bond"
	case 14:
		return "vlan"
	default:
		return fmt.Sprintf("type-%d", t)
	}
}

func networkDeviceStateLabel(s uint32) string {
	switch s {
	case 100:
		return "activated"
	case 70:
		return "ip-config"
	case 50:
		return "config"
	case 30:
		return "disconnected"
	case 20:
		return "unavailable"
	case 10:
		return "unmanaged"
	default:
		return fmt.Sprintf("state-%d", s)
	}
}

// moduleOverview builds a generic overview for a leaf module page.
func moduleOverview(ctx context.Context, registry *modules.Registry, id, heading, blurb, detail string) viewdata.PlaceholderData {
	return placeholderFor(ctx, registry, id, heading, blurb, detail)
}

// zfsPageData composes the ZFS page from the zfs module snapshot.
func zfsPageData(ctx context.Context, registry *modules.Registry) viewdata.ZFSPageData {
	data := viewdata.ZFSPageData{
		Heading: "ZFS",
		Blurb:   "Pool health, dataset usage, and scrub status",
		Detail:  "ZFS module unavailable",
	}
	if registry == nil {
		return data
	}
	m, ok := registry.Get("zfs")
	if !ok {
		return data
	}
	data.StatusBadge = badgeFor(m.Status(ctx))
	zm, isZFS := m.(*zfs.Module)
	if !isZFS {
		data.Detail = "invalid zfs module type"
		return data
	}
	snap, err := zm.Snapshot(ctx)
	if err != nil {
		data.Detail = err.Error()
		return data
	}
	if snap.StatusBadge == nil {
		snap.StatusBadge = data.StatusBadge
	}
	return snap
}

// cloudflarePageData composes the Cloudflare page from the cloudflare module snapshot.
func cloudflarePageData(ctx context.Context, registry *modules.Registry) viewdata.CloudflarePageData {
	data := viewdata.CloudflarePageData{
		Heading: "Cloudflare",
		Blurb:   "cloudflared tunnel agent and active tunnels",
		Detail:  "Cloudflare module unavailable",
	}
	if registry == nil {
		return data
	}
	m, ok := registry.Get("cloudflare")
	if !ok {
		return data
	}
	data.StatusBadge = badgeFor(m.Status(ctx))
	cm, isCF := m.(*cloudflare.Module)
	if !isCF {
		data.Detail = "invalid cloudflare module type"
		return data
	}
	snap, err := cm.Snapshot(ctx)
	if err != nil {
		data.Detail = err.Error()
		return data
	}
	if snap.StatusBadge == nil {
		snap.StatusBadge = data.StatusBadge
	}
	return snap
}

// podmanPageData composes the containers page from the podman module snapshot.
func podmanPageData(ctx context.Context, registry *modules.Registry) viewdata.PodmanPageData {
	data := viewdata.PodmanPageData{
		Heading: "Containers",
		Blurb:   "Podman runtime, images, and container lifecycle",
		Detail:  "Podman module unavailable",
	}
	if registry == nil {
		return data
	}

	m, ok := registry.Get("podman")
	if !ok {
		return data
	}

	data.StatusBadge = badgeFor(m.Status(ctx))
	pm, isPodman := m.(*podman.Module)
	if !isPodman {
		data.Detail = "invalid podman module type"
		return data
	}

	snap, err := pm.Snapshot(ctx)
	if err != nil {
		data.Detail = err.Error()
		return data
	}
	if snap.StatusBadge == nil {
		snap.StatusBadge = data.StatusBadge
	}
	return snap
}

// diagnosticsPageData renders a first-class diagnostics page payload and can
// be downloaded as a support bundle via /api/diagnostics/bundle.
func diagnosticsPageData(ctx context.Context, registry *modules.Registry) viewdata.DiagnosticsPageData {
	data := viewdata.DiagnosticsPageData{
		Heading:      "Diagnostics",
		Blurb:        "Host identity, uptime, and service diagnostics",
		Detail:       "Capture a support bundle with host snapshot + module status.",
		BundlePath:   "/api/diagnostics/bundle",
		BundleButton: "Download Bundle",
	}

	if registry == nil {
		return data
	}

	if m, ok := registry.Get("diagnostics"); ok {
		data.StatusBadge = badgeFor(m.Status(ctx))
		if diag, isDiag := m.(*diagnostics.Module); isDiag {
			populateDiagnosticsSnapshot(ctx, &data, diag)
		}
	}

	appendModuleStatuses(ctx, registry, &data)

	if data.Hostname == "" {
		host, _ := os.Hostname()
		if host == "" {
			host = "porcelain"
		}
		data.Hostname = host
	}

	return data
}

func populateDiagnosticsSnapshot(ctx context.Context, data *viewdata.DiagnosticsPageData, diag *diagnostics.Module) {
	snap, err := diag.Capture(ctx)
	if err != nil {
		return
	}

	data.Hostname = snap.Hostname
	data.Kernel = snap.Kernel
	data.OS = snap.OS
	data.Uptime = snap.Uptime.Truncate(time.Second).String()
	data.RunningServices = snap.RunningServices
	data.TotalServices = snap.TotalServices
	data.Services = make([]viewdata.DiagnosticsService, 0, len(snap.Services))
	for _, svc := range snap.Services {
		data.Services = append(data.Services, viewdata.DiagnosticsService{
			Name:        svc.Name,
			Description: svc.Description,
			Status:      svc.Status,
			Active:      svc.Active,
		})
	}
	data.JournalTail = append([]string(nil), snap.JournalTail...)
	data.OSTreeStatus = snap.OSTreeStatus
}

func appendModuleStatuses(ctx context.Context, registry *modules.Registry, data *viewdata.DiagnosticsPageData) {
	for _, id := range registry.SortedIDs() {
		m, ok := registry.Get(id)
		if !ok {
			continue
		}
		st := m.Status(ctx)
		data.Modules = append(data.Modules, viewdata.DiagnosticsModuleStatus{
			ID:     m.ID(),
			Name:   m.Name(),
			Health: string(st.Health),
			Detail: st.Detail,
		})
	}
}

// sensorsPageData composes the sensors page from the sensors module snapshot.
func sensorsPageData(ctx context.Context, registry *modules.Registry) viewdata.SensorsPageData {
	data := viewdata.SensorsPageData{
		Heading: "Sensors",
		Blurb:   "Hardware telemetry via lm-sensors",
		Detail:  "Sensors module unavailable",
	}
	if registry == nil {
		return data
	}

	m, ok := registry.Get("sensors")
	if !ok {
		return data
	}
	data.StatusBadge = badgeFor(m.Status(ctx))

	sm, isSensors := m.(*sensors.Module)
	if !isSensors {
		data.Detail = "invalid sensors module type"
		return data
	}

	snap, err := sm.Snapshot(ctx)
	if err != nil {
		data.Detail = err.Error()
		return data
	}
	if snap.StatusBadge == nil {
		snap.StatusBadge = data.StatusBadge
	}
	return snap
}

// diagnosticsBundle returns a tar.gz support bundle with snapshot and module
// status. It is intentionally read-only and safe to run on developer hosts.
func diagnosticsBundle(ctx context.Context, registry *modules.Registry) ([]byte, string, error) {
	page := diagnosticsPageData(ctx, registry)
	bundleTS := time.Now().UTC().Format("20060102T150405Z")

	snapshot := map[string]any{
		"generated_at":       time.Now().UTC().Format(time.RFC3339),
		"hostname":           page.Hostname,
		"kernel":             page.Kernel,
		"os":                 page.OS,
		"uptime":             page.Uptime,
		"running_services":   page.RunningServices,
		"total_services":     page.TotalServices,
		"services":           page.Services,
		"journal_tail":       page.JournalTail,
		"rpm_ostree_status":  page.OSTreeStatus,
		"module_status_list": page.Modules,
	}

	snapshotJSON, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("marshal diagnostics snapshot: %w", err)
	}

	moduleJSON, err := json.MarshalIndent(page.Modules, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("marshal module status: %w", err)
	}

	manifest := fmt.Sprintf(
		"Porcelain diagnostics bundle\nGenerated: %s\nHostname: %s\n",
		time.Now().UTC().Format(time.RFC3339),
		page.Hostname,
	)

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	addFile := func(name string, body []byte) error {
		hdr := &tar.Header{
			Name:    name,
			Mode:    0o644,
			Size:    int64(len(body)),
			ModTime: time.Now(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err := tw.Write(body)
		return err
	}

	if err := addFile("manifest.txt", []byte(manifest)); err != nil {
		return nil, "", fmt.Errorf("write manifest: %w", err)
	}
	if err := addFile("snapshot.json", snapshotJSON); err != nil {
		return nil, "", fmt.Errorf("write snapshot: %w", err)
	}
	if err := addFile("module-status.json", moduleJSON); err != nil {
		return nil, "", fmt.Errorf("write module status: %w", err)
	}

	if err := tw.Close(); err != nil {
		return nil, "", fmt.Errorf("close tar writer: %w", err)
	}
	if err := gzw.Close(); err != nil {
		return nil, "", fmt.Errorf("close gzip writer: %w", err)
	}

	return buf.Bytes(), fmt.Sprintf("porcelain-diagnostics-%s.tar.gz", bundleTS), nil
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
	page, currentID, title string,
	build func(c fiber.Ctx) any,
) fiber.Handler {
	return func(c fiber.Ctx) error {
		data := templates.TemplateData{
			Title:         title,
			CurrentModule: currentID,
			Modules:       sidebarModules(c.Context(), registry, currentID),
			User:          buildUserInfo(c, cfg),
			Data:          build(c),
		}

		// X-Page-Title lets the HTMX afterSwap handler update the breadcrumb
		// in the fixed header without a full-page reload.
		c.Set("X-Page-Title", title)
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
		// no-cache forces revalidation each request so dev CSS edits surface
		// immediately. Embedded asset reads are O(memcpy); the bandwidth hit is
		// negligible for the porcelain UI footprint.
		c.Set("Cache-Control", "no-cache")
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
// from the registered modules. Group entries (those with Children) get a
// rolled-up badge: worst-of (Unavailable > Degraded > OK) across registered
// children. Children with no registered module are skipped from the rollup.
func sidebarModules(ctx context.Context, registry *modules.Registry, currentID string) []templates.ModuleNav {
	nav := viewdata.SidebarModules(currentID)
	if registry == nil {
		return nav
	}
	for i := range nav {
		// Apply child badges first.
		for j := range nav[i].Children {
			if m, ok := registry.Get(nav[i].Children[j].ID); ok {
				nav[i].Children[j].StatusBadge = badgeFor(m.Status(ctx))
			}
		}
		// Then determine the parent's badge: explicit module status if
		// registered, otherwise rolled up from children.
		if m, ok := registry.Get(nav[i].ID); ok {
			nav[i].StatusBadge = badgeFor(m.Status(ctx))
		} else if len(nav[i].Children) > 0 {
			nav[i].StatusBadge = rolledUpBadge(ctx, registry, nav[i].Children)
		}
	}
	return nav
}

// rolledUpBadge returns the worst-of-children badge (Unavailable > Degraded >
// OK). Returns nil when no child is registered.
func rolledUpBadge(ctx context.Context, registry *modules.Registry, children []templates.ModuleNav) *templates.StatusBadge {
	worst := -1
	for _, ch := range children {
		m, ok := registry.Get(ch.ID)
		if !ok {
			continue
		}
		rank := healthRank(m.Status(ctx).Health)
		if rank > worst {
			worst = rank
		}
	}
	switch worst {
	case 0:
		return &templates.StatusBadge{Text: "ok", Class: "badge-success"}
	case 1:
		return &templates.StatusBadge{Text: "dev", Class: "badge-warning"}
	case 2:
		return &templates.StatusBadge{Text: "off", Class: "badge-danger"}
	default:
		return nil
	}
}

// healthRank orders Health values so worst-of comparisons are numeric.
func healthRank(h modules.Health) int {
	switch h {
	case modules.HealthOK:
		return 0
	case modules.HealthDegraded:
		return 1
	case modules.HealthUnavailable:
		return 2
	default:
		return -1
	}
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
