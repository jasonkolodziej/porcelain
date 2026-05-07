package config

import (
	"os"
	"strings"
)

// Config contains the top-level runtime configuration for the Porcelain daemon.
type Config struct {
	Server       ServerConfig
	Auth         AuthConfig
	DBus         DBusConfig
	Secrets      SecretsConfig
	Certificates CertificatesConfig
}

// ServerConfig controls the HTTP listener and socket activation behavior.
type ServerConfig struct {
	Address                string
	EnableSocketActivation bool
}

// AuthConfig defines the Dex-facing settings used by the scaffolded middleware.
type AuthConfig struct {
	Enabled          bool
	IssuerURL        string
	ClientID         string
	ClientSecretPath string
	RedirectURL      string
	GroupsClaim      string
	AdminGroup       string
}

// DBusConfig controls how the daemon reaches host services on the bus.
type DBusConfig struct {
	Address                string
	Bus                    string
	EnforcePeerCredentials bool
	Optional               bool
}

// SecretsConfig selects the secret backend and backend-specific hints.
type SecretsConfig struct {
	Backend         string
	StorageDir      string
	AgeIdentityPath string
	AgeRecipient    string
}

// CertificatesConfig selects the certificate manager strategy and bootstrap identity.
type CertificatesConfig struct {
	Backend    string
	CommonName string
	DNSNames   []string
	ValidDays  int
	// Directory holds pre-provisioned PKI material when the filesystem backend
	// is selected. Expected files: server.crt, server.key, ca.crt.
	Directory string
}

// LoadFromEnv returns a runnable configuration with sensible scaffold defaults.
func LoadFromEnv() Config {
	return Config{
		Server: ServerConfig{
			Address:                envOrDefault("PORCELAIN_ADDRESS", ":8443"),
			EnableSocketActivation: envOrDefault("PORCELAIN_SOCKET_ACTIVATION", "true") == "true",
		},
		Auth: AuthConfig{
			Enabled:          envOrDefault("PORCELAIN_AUTH_ENABLED", "false") == "true",
			IssuerURL:        envOrDefault("PORCELAIN_DEX_ISSUER", "https://dex.example.com"),
			ClientID:         envOrDefault("PORCELAIN_DEX_CLIENT_ID", "porcelain"),
			ClientSecretPath: envOrDefault("PORCELAIN_DEX_CLIENT_SECRET_PATH", "auth/dex/client-secret"),
			RedirectURL:      envOrDefault("PORCELAIN_DEX_REDIRECT_URL", "https://localhost/callback"),
			GroupsClaim:      envOrDefault("PORCELAIN_DEX_GROUPS_CLAIM", "groups"),
			AdminGroup:       envOrDefault("PORCELAIN_ADMIN_GROUP", "porcelain-admins"),
		},
		DBus: DBusConfig{
			Address:                envOrDefault("PORCELAIN_DBUS_ADDRESS", ""),
			Bus:                    envOrDefault("PORCELAIN_DBUS_BUS", "system"),
			EnforcePeerCredentials: envOrDefault("PORCELAIN_DBUS_ENFORCE_PEER_CREDS", "true") == "true",
			Optional:               envOrDefault("PORCELAIN_DBUS_OPTIONAL", "false") == "true",
		},
		Secrets: SecretsConfig{
			Backend:         envOrDefault("PORCELAIN_SECRETS_BACKEND", "memory"),
			StorageDir:      envOrDefault("PORCELAIN_SECRETS_DIR", "/var/lib/porcelain/secrets"),
			AgeIdentityPath: envOrDefault("PORCELAIN_AGE_IDENTITY", ""),
			AgeRecipient:    envOrDefault("PORCELAIN_AGE_RECIPIENT", ""),
		},
		Certificates: CertificatesConfig{
			Backend:    envOrDefault("PORCELAIN_CERTIFICATES_BACKEND", "selfsigned"),
			CommonName: envOrDefault("PORCELAIN_TLS_COMMON_NAME", "localhost"),
			DNSNames:   csvOrDefault("PORCELAIN_TLS_DNS_NAMES", []string{"localhost"}),
			ValidDays:  30,
			Directory:  envOrDefault("PORCELAIN_TLS_DIR", "/pki/server"),
		},
	}
}

// envOrDefault reads an environment variable and falls back when it is unset.
func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}

// csvOrDefault splits a comma-separated environment variable or returns the fallback values.
func csvOrDefault(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return fallback
	}

	return out
}
