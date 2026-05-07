package secrets

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"time"
)

// ErrNotImplemented marks scaffold hooks that still need a concrete backend implementation.
var ErrNotImplemented = errors.New("backend is scaffolded but not implemented")

// SecretValue is the atomic secret payload and its associated metadata.
type SecretValue struct {
	Data      []byte
	Version   string
	CreatedAt time.Time
	ExpiresAt *time.Time
	Metadata  map[string]string
}

// SecretStore abstracts the runtime backend used to persist or retrieve secrets.
type SecretStore interface {
	Get(ctx context.Context, path string) (*SecretValue, error)
	Put(ctx context.Context, path string, value *SecretValue) error
	Delete(ctx context.Context, path string) error
	List(ctx context.Context, prefix string) ([]string, error)
	Watch(ctx context.Context, path string) (<-chan *SecretValue, error)
}

// CertificateStore manages server certificates and trust bundles on top of a secret backend.
type CertificateStore interface {
	SecretStore
	GetCertificate(ctx context.Context, name string) (*tls.Certificate, error)
	GetCA(ctx context.Context, name string) (*x509.CertPool, error)
	Issue(ctx context.Context, req CertificateRequest) (*tls.Certificate, error)
	Renew(ctx context.Context, name string) error
	GetExpiry(ctx context.Context, name string) (time.Duration, error)
}

// CertificateRequest describes the parameters used when issuing or renewing a certificate.
type CertificateRequest struct {
	CommonName  string
	DNSNames    []string
	IPAddresses []net.IP
	Validity    time.Duration
	IsCA        bool
	KeyType     KeyType
	KeySize     int
	IssuerName  string
}

// KeyType identifies the asymmetric algorithm requested for a certificate key.
type KeyType int

const (
	// KeyTypeRSA requests an RSA private key.
	KeyTypeRSA KeyType = iota
	// KeyTypeECDSA requests an ECDSA private key.
	KeyTypeECDSA
	// KeyTypeEd25519 requests an Ed25519 private key.
	KeyTypeEd25519
)
