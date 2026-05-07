package secrets

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"time"

	secretspkg "github.com/jasonkolodziej/porcelain/porcelain/pkg/secrets"
)

// CertManagerStore reserves the certificate issuance boundary for Kubernetes and container deployments.
type CertManagerStore struct {
	secretspkg.SecretStore
}

// NewCertManagerStore returns a certificate manager placeholder backed by the selected secret store.
func NewCertManagerStore(secretStore secretspkg.SecretStore) *CertManagerStore {
	return &CertManagerStore{SecretStore: secretStore}
}

// GetCertificate reports that the cert-manager issuer still needs its concrete implementation.
func (c *CertManagerStore) GetCertificate(_ context.Context, _ string) (*tls.Certificate, error) {
	return nil, secretspkg.ErrNotImplemented
}

// GetCA reports that the cert-manager issuer still needs its concrete implementation.
func (c *CertManagerStore) GetCA(_ context.Context, _ string) (*x509.CertPool, error) {
	return nil, secretspkg.ErrNotImplemented
}

// Issue reports that the cert-manager issuer still needs its concrete implementation.
func (c *CertManagerStore) Issue(_ context.Context, _ secretspkg.CertificateRequest) (*tls.Certificate, error) {
	return nil, secretspkg.ErrNotImplemented
}

// Renew reports that the cert-manager issuer still needs its concrete implementation.
func (c *CertManagerStore) Renew(_ context.Context, _ string) error {
	return secretspkg.ErrNotImplemented
}

// GetExpiry reports that the cert-manager issuer still needs its concrete implementation.
func (c *CertManagerStore) GetExpiry(_ context.Context, _ string) (time.Duration, error) {
	return 0, secretspkg.ErrNotImplemented
}
