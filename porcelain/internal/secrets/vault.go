package secrets

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"time"

	secretspkg "github.com/jasonkolodziej/porcelain/porcelain/pkg/secrets"
)

// VaultStore reserves the production backend boundary for HashiCorp Vault integration.
type VaultStore struct{}

// NewVaultStore returns a scaffolded backend placeholder until Vault wiring is added.
func NewVaultStore() (*VaultStore, error) {
	return &VaultStore{}, nil
}

// Get reports that the Vault backend still needs its concrete implementation.
func (v *VaultStore) Get(_ context.Context, _ string) (*secretspkg.SecretValue, error) {
	return nil, secretspkg.ErrNotImplemented
}

// Put reports that the Vault backend still needs its concrete implementation.
func (v *VaultStore) Put(_ context.Context, _ string, _ *secretspkg.SecretValue) error {
	return secretspkg.ErrNotImplemented
}

// Delete reports that the Vault backend still needs its concrete implementation.
func (v *VaultStore) Delete(_ context.Context, _ string) error {
	return secretspkg.ErrNotImplemented
}

// List reports that the Vault backend still needs its concrete implementation.
func (v *VaultStore) List(_ context.Context, _ string) ([]string, error) {
	return nil, secretspkg.ErrNotImplemented
}

// Watch reports that the Vault backend still needs its concrete implementation.
func (v *VaultStore) Watch(_ context.Context, _ string) (<-chan *secretspkg.SecretValue, error) {
	return nil, secretspkg.ErrNotImplemented
}

// VaultCertificateStore reserves the PKI issuer boundary for Vault-backed certificates.
type VaultCertificateStore struct {
	secretspkg.SecretStore
}

// NewVaultCertificateStore returns a certificate manager placeholder backed by the selected secret store.
func NewVaultCertificateStore(secretStore secretspkg.SecretStore) *VaultCertificateStore {
	return &VaultCertificateStore{SecretStore: secretStore}
}

// GetCertificate reports that the Vault PKI issuer still needs its concrete implementation.
func (v *VaultCertificateStore) GetCertificate(_ context.Context, _ string) (*tls.Certificate, error) {
	return nil, secretspkg.ErrNotImplemented
}

// GetCA reports that the Vault PKI issuer still needs its concrete implementation.
func (v *VaultCertificateStore) GetCA(_ context.Context, _ string) (*x509.CertPool, error) {
	return nil, secretspkg.ErrNotImplemented
}

// Issue reports that the Vault PKI issuer still needs its concrete implementation.
func (v *VaultCertificateStore) Issue(_ context.Context, _ secretspkg.CertificateRequest) (*tls.Certificate, error) {
	return nil, secretspkg.ErrNotImplemented
}

// Renew reports that the Vault PKI issuer still needs its concrete implementation.
func (v *VaultCertificateStore) Renew(_ context.Context, _ string) error {
	return secretspkg.ErrNotImplemented
}

// GetExpiry reports that the Vault PKI issuer still needs its concrete implementation.
func (v *VaultCertificateStore) GetExpiry(_ context.Context, _ string) (time.Duration, error) {
	return 0, secretspkg.ErrNotImplemented
}
