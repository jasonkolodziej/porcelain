package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"

	"github.com/gofiber/fiber/v3"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/auth"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/certmanager"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/config"
	secretspkg "github.com/jasonkolodziej/porcelain/porcelain/pkg/secrets"
)

// Server owns the Fiber application, listener, and TLS policy.
type Server struct {
	config    config.Config
	app       *fiber.App
	tlsConfig *tls.Config
}

// New assembles the Fiber server, route set, and mandatory mTLS listener policy.
func New(cfg config.Config, dexAuth *auth.DexAuth, certStore secretspkg.CertificateStore, tlsManager *certmanager.TLSManager) (*Server, error) {
	app, err := NewRouter(cfg, dexAuth)
	if err != nil {
		return nil, err
	}

	clientCAs, err := certStore.GetCA(context.Background(), cfg.Certificates.CommonName)
	if err != nil {
		return nil, fmt.Errorf("load client CA bundle: %w", err)
	}

	return &Server{
		config: cfg,
		app:    app,
		tlsConfig: &tls.Config{
			MinVersion:               tls.VersionTLS13,
			NextProtos:               []string{"h2", "http/1.1"},
			GetCertificate:           tlsManager.GetCertificate,
			ClientAuth:               tls.RequireAndVerifyClientCert,
			ClientCAs:                clientCAs,
			PreferServerCipherSuites: true,
		},
	}, nil
}

// Start opens the configured listener, wraps it with mandatory mTLS, and serves the Fiber application.
func (s *Server) Start(ctx context.Context) error {
	listener, err := Listen(ctx, s.config.Server)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	tlsListener := tls.NewListener(listener, s.tlsConfig)

	return s.app.Listener(tlsListener, fiber.ListenConfig{
		DisableStartupMessage: true,
	})
}

// Listener returns the underlying network listener used by the Fiber server.
func (s *Server) Listener(ctx context.Context) (net.Listener, error) {
	return Listen(ctx, s.config.Server)
}
