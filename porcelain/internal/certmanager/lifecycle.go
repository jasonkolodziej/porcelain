package certmanager

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	secretspkg "github.com/jasonkolodziej/porcelain/porcelain/pkg/secrets"
)

// TLSManager keeps the active certificate hot in memory and refreshes it before expiry.
type TLSManager struct {
	store    secretspkg.CertificateStore
	certName string

	mu   sync.RWMutex
	cert *tls.Certificate
}

// NewTLSManager loads or bootstraps the certificate that the HTTP server will present.
func NewTLSManager(ctx context.Context, store secretspkg.CertificateStore, certName string) (*TLSManager, error) {
	manager := &TLSManager{
		store:    store,
		certName: certName,
	}

	if err := manager.loadOrBootstrap(ctx); err != nil {
		return nil, err
	}

	return manager, nil
}

// GetCertificate returns the in-memory leaf certificate to the TLS handshake.
func (m *TLSManager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.cert == nil {
		return nil, fmt.Errorf("certificate %q is not loaded", m.certName)
	}

	return m.cert, nil
}

// StartRenewalLoop refreshes the certificate periodically so the process does not need a restart.
func (m *TLSManager) StartRenewalLoop(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := m.store.Renew(ctx, m.certName); err != nil {
					continue
				}
				_ = m.loadOrBootstrap(ctx)
			}
		}
	}()
}

// loadOrBootstrap ensures the requested certificate exists and is ready for live handshakes.
func (m *TLSManager) loadOrBootstrap(ctx context.Context) error {
	cert, err := m.store.GetCertificate(ctx, m.certName)
	if err != nil {
		_, issueErr := m.store.Issue(ctx, secretspkg.CertificateRequest{
			CommonName: m.certName,
			DNSNames:   []string{m.certName, "localhost"},
			Validity:   30 * 24 * time.Hour,
		})
		if issueErr != nil {
			return fmt.Errorf("issue bootstrap certificate: %w", issueErr)
		}

		cert, err = m.store.GetCertificate(ctx, m.certName)
		if err != nil {
			return fmt.Errorf("load bootstrap certificate: %w", err)
		}
	}

	m.mu.Lock()
	m.cert = cert
	m.mu.Unlock()

	return nil
}
