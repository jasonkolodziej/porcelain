package app

import (
	"context"
	"fmt"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/auth"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/certmanager"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/config"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/dbus"
	runtimeSecrets "github.com/jasonkolodziej/porcelain/porcelain/internal/secrets"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/server"
	secretspkg "github.com/jasonkolodziej/porcelain/porcelain/pkg/secrets"
)

// Application captures the runtime dependencies needed to serve the dashboard.
type Application struct {
	Config      config.Config
	DBus        *dbus.ConnectionManager
	SecretStore secretspkg.SecretStore
	CertStore   secretspkg.CertificateStore
	TLSManager  *certmanager.TLSManager
	Auth        *auth.DexAuth
	Server      *server.Server
}

// Run assembles the runtime dependencies from configuration and starts the HTTP server.
func Run(ctx context.Context) error {
	cfg := config.LoadFromEnv()

	application, err := New(ctx, cfg)
	if err != nil {
		return err
	}

	return application.Start(ctx)
}

// New wires the core scaffolding so the README architecture is reflected in code.
func New(ctx context.Context, cfg config.Config) (*Application, error) {
	dbusManager := dbus.NewConnectionManager(dbus.ConnectionConfig{
		Address:                cfg.DBus.Address,
		Bus:                    dbus.BusType(cfg.DBus.Bus),
		EnforcePeerCredentials: cfg.DBus.EnforcePeerCredentials,
	})
	if err := dbusManager.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect to D-Bus: %w", err)
	}

	secretStore, err := runtimeSecrets.NewSecretStore(cfg.Secrets)
	if err != nil {
		return nil, fmt.Errorf("create secret store: %w", err)
	}

	certStore, err := runtimeSecrets.NewCertificateStore(secretStore, cfg.Certificates)
	if err != nil {
		return nil, fmt.Errorf("create certificate store: %w", err)
	}

	tlsManager, err := certmanager.NewTLSManager(ctx, certStore, cfg.Certificates.CommonName)
	if err != nil {
		return nil, fmt.Errorf("create TLS manager: %w", err)
	}

	dexAuth := auth.NewDexAuth(cfg.Auth, secretStore)
	httpServer, err := server.New(cfg, dexAuth, certStore, tlsManager)
	if err != nil {
		return nil, fmt.Errorf("create server: %w", err)
	}

	return &Application{
		Config:      cfg,
		DBus:        dbusManager,
		SecretStore: secretStore,
		CertStore:   certStore,
		TLSManager:  tlsManager,
		Auth:        dexAuth,
		Server:      httpServer,
	}, nil
}

// Start begins serving requests with the configured listeners, auth middleware, and certificate manager.
func (a *Application) Start(ctx context.Context) error {
	return a.Server.Start(ctx)
}
