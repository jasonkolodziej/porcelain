package auth

import (
	"context"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/config"
	secretspkg "github.com/jasonkolodziej/porcelain/porcelain/pkg/secrets"
)

type contextKey string

const claimsContextKey contextKey = "claims"
const claimsLocalsKey = "claims"

// Claims carries the authenticated identity data needed by downstream handlers.
type Claims struct {
	Subject string
	Groups  []string
	Token   string
}

// DexAuth owns the scaffolded auth middleware and the backing secret store reference.
type DexAuth struct {
	config      config.AuthConfig
	secretStore secretspkg.SecretStore
}

// NewDexAuth creates the auth middleware dependency without forcing the rest of the system to know Dex details.
func NewDexAuth(cfg config.AuthConfig, secretStore secretspkg.SecretStore) *DexAuth {
	return &DexAuth{
		config:      cfg,
		secretStore: secretStore,
	}
}

// Middleware attaches the authenticated claims to the Fiber request context when auth is enabled.
func (d *DexAuth) Middleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		if !d.config.Enabled {
			return c.Next()
		}

		claims, ok := d.extractClaims(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).SendString("missing bearer token")
		}

		fiber.Locals[Claims](c, claimsLocalsKey, claims)
		ctx := context.WithValue(c.Context(), claimsContextKey, claims)
		c.SetContext(ctx)

		return c.Next()
	}
}

// SecretStore exposes the configured secret store to future Dex token verification code.
func (d *DexAuth) SecretStore() secretspkg.SecretStore {
	return d.secretStore
}

// ClaimsFromContext returns the auth claims when middleware has attached them to the request.
func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(Claims)
	return claims, ok
}

// ClaimsFromFiber returns the auth claims when Fiber middleware has attached them to the request.
func ClaimsFromFiber(c fiber.Ctx) (Claims, bool) {
	claims, ok := c.Locals(claimsLocalsKey).(Claims)
	if !ok {
		return Claims{}, false
	}

	return claims, true
}

// extractClaims derives minimal identity information from headers until full OIDC verification is wired in.
func (d *DexAuth) extractClaims(c fiber.Ctx) (Claims, bool) {
	header := strings.TrimSpace(c.Get("Authorization"))
	if header == "" {
		return Claims{}, false
	}

	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if token == "" || token == header {
		return Claims{}, false
	}

	groupsHeader := strings.TrimSpace(c.Get("X-Porcelain-Groups"))
	groups := make([]string, 0)
	if groupsHeader != "" {
		for _, group := range strings.Split(groupsHeader, ",") {
			trimmed := strings.TrimSpace(group)
			if trimmed != "" {
				groups = append(groups, trimmed)
			}
		}
	}

	subject := strings.TrimSpace(c.Get("X-Porcelain-Subject"))
	if subject == "" {
		subject = "unknown"
	}

	return Claims{
		Subject: subject,
		Groups:  groups,
		Token:   token,
	}, true
}
