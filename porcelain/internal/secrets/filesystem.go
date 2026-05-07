package secrets

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	secretspkg "github.com/jasonkolodziej/porcelain/porcelain/pkg/secrets"
)

// FilesystemCertificateStore serves a server certificate and a client-trust
// CA bundle from a directory on disk. It is intended for container, ignition,
// and manual development workflows where the operator (or build pipeline)
// pre-provisions the PKI material.
//
// Expected layout, relative to the configured directory:
//
//	server.crt  - PEM-encoded server leaf (and any intermediates)
//	server.key  - PEM-encoded server private key
//	ca.crt      - PEM-encoded client trust roots (one or more CAs)
type FilesystemCertificateStore struct {
	secretspkg.SecretStore
	dir string

	mu     sync.RWMutex
	server tls.Certificate
	caPool *x509.CertPool
	leaf   *x509.Certificate
}

// NewFilesystemCertificateStore loads the static PKI material once at startup
// and caches it for serving requests and TLS handshakes.
func NewFilesystemCertificateStore(secretStore secretspkg.SecretStore, dir string) (*FilesystemCertificateStore, error) {
	if dir == "" {
		return nil, errors.New("filesystem certificate store requires a directory")
	}

	store := &FilesystemCertificateStore{
		SecretStore: secretStore,
		dir:         filepath.Clean(dir),
	}
	if err := store.reload(); err != nil {
		return nil, err
	}

	return store, nil
}

// GetCertificate returns the loaded server certificate; the name is ignored
// because this backend serves a single identity per directory.
func (s *FilesystemCertificateStore) GetCertificate(_ context.Context, _ string) (*tls.Certificate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.server.Leaf == nil {
		return nil, errors.New("server certificate not loaded")
	}
	cert := s.server
	return &cert, nil
}

// GetCA returns the configured client trust pool.
func (s *FilesystemCertificateStore) GetCA(_ context.Context, _ string) (*x509.CertPool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.caPool == nil {
		return nil, errors.New("client CA bundle not loaded")
	}
	return s.caPool, nil
}

// Issue is unsupported for the filesystem backend; PKI material is provisioned
// out of band so the daemon stays a strict consumer.
func (s *FilesystemCertificateStore) Issue(_ context.Context, _ secretspkg.CertificateRequest) (*tls.Certificate, error) {
	return nil, fmt.Errorf("filesystem certificate store: %w", secretspkg.ErrNotImplemented)
}

// Renew triggers a reload from disk so operators can rotate certificates by
// replacing the files and SIGHUP'ing the process from a future control flow.
func (s *FilesystemCertificateStore) Renew(_ context.Context, _ string) error {
	return s.reload()
}

// GetExpiry returns the remaining lifetime of the loaded server leaf.
func (s *FilesystemCertificateStore) GetExpiry(_ context.Context, _ string) (time.Duration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.leaf == nil {
		return 0, errors.New("server certificate not loaded")
	}
	return time.Until(s.leaf.NotAfter), nil
}

// reload re-reads the configured directory under a write lock so callers see
// either the previous bundle or the new bundle, never a torn state.
func (s *FilesystemCertificateStore) reload() error {
	certPath := filepath.Join(s.dir, "server.crt")
	keyPath := filepath.Join(s.dir, "server.key")
	caPath := filepath.Join(s.dir, "ca.crt")

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", certPath, err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", keyPath, err)
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", caPath, err)
	}

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("parse server key pair: %w", err)
	}
	if len(pair.Certificate) > 0 {
		pair.Leaf, _ = x509.ParseCertificate(pair.Certificate[0])
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return fmt.Errorf("append client CA bundle from %s", caPath)
	}

	s.mu.Lock()
	s.server = pair
	s.caPool = pool
	s.leaf = pair.Leaf
	s.mu.Unlock()

	return nil
}
