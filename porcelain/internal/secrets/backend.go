package secrets

import (
	"fmt"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/config"
	secretspkg "github.com/jasonkolodziej/porcelain/porcelain/pkg/secrets"
)

// NewSecretStore selects the runtime secret backend configured for the daemon.
func NewSecretStore(cfg config.SecretsConfig) (secretspkg.SecretStore, error) {
	switch cfg.Backend {
	case "", "memory":
		return NewMemoryStore(), nil
	case "systemd-creds":
		return NewSystemdCredsStore(cfg.StorageDir)
	case "age":
		return NewAgeStore(cfg.StorageDir, cfg.AgeIdentityPath, cfg.AgeRecipient)
	case "vault":
		return NewVaultStore()
	default:
		return nil, fmt.Errorf("unsupported secrets backend %q", cfg.Backend)
	}
}

// NewCertificateStore selects the certificate lifecycle strategy layered on top of the chosen secret store.
func NewCertificateStore(secretStore secretspkg.SecretStore, cfg config.CertificatesConfig) (secretspkg.CertificateStore, error) {
	switch cfg.Backend {
	case "", "selfsigned":
		return NewSelfSignedStore(secretStore, cfg.CommonName, cfg.DNSNames, cfg.ValidDays), nil
	case "filesystem":
		return NewFilesystemCertificateStore(secretStore, cfg.Directory)
	case "cert-manager":
		return NewCertManagerStore(secretStore), nil
	case "vault-pki":
		return NewVaultCertificateStore(secretStore), nil
	default:
		return nil, fmt.Errorf("unsupported certificates backend %q", cfg.Backend)
	}
}
