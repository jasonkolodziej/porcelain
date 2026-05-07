package auth_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/auth"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/config"
)

// TestDexMiddlewareDisabledPasses confirms that auth boundary stays opt-in
// while the Dex integration is being built out.
func TestDexMiddlewareDisabledPasses(t *testing.T) {
	dex := auth.NewDexAuth(config.AuthConfig{Enabled: false}, nil)

	app := fiber.New(fiber.Config{})
	app.Use(dex.Middleware())
	app.Get("/", func(c fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

// TestDexMiddlewareRejectsMissingToken proves the middleware short-circuits
// requests that lack a bearer token once auth is required.
func TestDexMiddlewareRejectsMissingToken(t *testing.T) {
	dex := auth.NewDexAuth(config.AuthConfig{Enabled: true}, nil)

	app := fiber.New(fiber.Config{})
	app.Use(dex.Middleware())
	app.Get("/", func(c fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

// TestDexMiddlewareSurfacesClaims confirms the scaffolded header pathway hands
// claims down to handlers via Fiber locals.
func TestDexMiddlewareSurfacesClaims(t *testing.T) {
	dex := auth.NewDexAuth(config.AuthConfig{Enabled: true}, nil)

	app := fiber.New(fiber.Config{})
	app.Use(dex.Middleware())
	app.Get("/", func(c fiber.Ctx) error {
		claims, ok := auth.ClaimsFromFiber(c)
		if !ok {
			return c.Status(fiber.StatusInternalServerError).SendString("missing")
		}
		return c.SendString(claims.Subject + "|" + strings.Join(claims.Groups, ","))
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("X-Porcelain-Subject", "alice")
	req.Header.Set("X-Porcelain-Groups", "porcelain-admins,operators")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(data) != "alice|porcelain-admins,operators" {
		t.Fatalf("body: %q", data)
	}
}
