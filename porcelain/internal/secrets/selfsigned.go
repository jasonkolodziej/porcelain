package secrets

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	secretspkg "github.com/jasonkolodziej/porcelain/porcelain/pkg/secrets"
)

const (
	certificatePEMFile = "cert.pem"
	privateKeyPEMFile  = "key.pem"
)

// SelfSignedStore issues and rotates local certificates on top of the configured secret backend.
type SelfSignedStore struct {
	secretspkg.SecretStore
	defaultCommonName string
	defaultDNSNames   []string
	validDays         int
}

// NewSelfSignedStore creates the bootstrap certificate manager used by the initial scaffold.
func NewSelfSignedStore(secretStore secretspkg.SecretStore, commonName string, dnsNames []string, validDays int) *SelfSignedStore {
	return &SelfSignedStore{
		SecretStore:       secretStore,
		defaultCommonName: commonName,
		defaultDNSNames:   dnsNames,
		validDays:         validDays,
	}
}

// GetCertificate loads the named certificate bundle from the underlying secret backend.
func (s *SelfSignedStore) GetCertificate(ctx context.Context, name string) (*tls.Certificate, error) {
	certValue, err := s.Get(ctx, certificatePath(name, certificatePEMFile))
	if err != nil {
		return nil, err
	}

	keyValue, err := s.Get(ctx, certificatePath(name, privateKeyPEMFile))
	if err != nil {
		return nil, err
	}

	pair, err := tls.X509KeyPair(certValue.Data, keyValue.Data)
	if err != nil {
		return nil, fmt.Errorf("parse certificate key pair: %w", err)
	}
	if len(pair.Certificate) > 0 {
		pair.Leaf, _ = x509.ParseCertificate(pair.Certificate[0])
	}

	return &pair, nil
}

// GetCA builds a trust pool containing the active bootstrap certificate.
func (s *SelfSignedStore) GetCA(ctx context.Context, name string) (*x509.CertPool, error) {
	certValue, err := s.Get(ctx, certificatePath(name, certificatePEMFile))
	if err != nil {
		return nil, err
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certValue.Data) {
		return nil, fmt.Errorf("append CA bundle for %q", name)
	}

	return pool, nil
}

// Issue generates a fresh self-signed certificate and persists both the leaf and private key.
func (s *SelfSignedStore) Issue(ctx context.Context, req secretspkg.CertificateRequest) (*tls.Certificate, error) {
	certName := req.CommonName
	if certName == "" {
		certName = s.defaultCommonName
	}

	validFor := req.Validity
	if validFor == 0 {
		validFor = time.Duration(s.validDays) * 24 * time.Hour
	}

	dnsNames := req.DNSNames
	if len(dnsNames) == 0 {
		dnsNames = append([]string(nil), s.defaultDNSNames...)
	}

	certificatePEM, keyPEM, pair, err := issueSelfSignedPair(certName, dnsNames, validFor, req.KeyType)
	if err != nil {
		return nil, err
	}

	if err := s.Put(ctx, certificatePath(certName, certificatePEMFile), &secretspkg.SecretValue{Data: certificatePEM}); err != nil {
		return nil, err
	}
	if err := s.Put(ctx, certificatePath(certName, privateKeyPEMFile), &secretspkg.SecretValue{Data: keyPEM}); err != nil {
		return nil, err
	}

	return pair, nil
}

// Renew replaces the named certificate with a newly issued self-signed bundle.
func (s *SelfSignedStore) Renew(ctx context.Context, name string) error {
	_, err := s.Issue(ctx, secretspkg.CertificateRequest{
		CommonName: name,
		DNSNames:   s.defaultDNSNames,
		Validity:   time.Duration(s.validDays) * 24 * time.Hour,
	})
	return err
}

// GetExpiry returns the remaining lifetime of the active certificate.
func (s *SelfSignedStore) GetExpiry(ctx context.Context, name string) (time.Duration, error) {
	cert, err := s.GetCertificate(ctx, name)
	if err != nil {
		return 0, err
	}
	if cert.Leaf == nil {
		return 0, fmt.Errorf("certificate %q does not have a parsed leaf", name)
	}

	return time.Until(cert.Leaf.NotAfter), nil
}

// certificatePath normalizes how certificate bundles are stored inside the secret backend.
func certificatePath(name, file string) string {
	return fmt.Sprintf("certificates/%s/%s", name, file)
}

// issueSelfSignedPair creates a PEM-encoded key pair suitable for bootstrap TLS and mTLS testing.
func issueSelfSignedPair(commonName string, dnsNames []string, validFor time.Duration, keyType secretspkg.KeyType) ([]byte, []byte, *tls.Certificate, error) {
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate serial number: %w", err)
	}

	notBefore := time.Now().UTC().Add(-5 * time.Minute)
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             notBefore,
		NotAfter:              notBefore.Add(validFor),
		DNSNames:              dnsNames,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	var privateKey any
	var privateKeyPEM []byte

	switch keyType {
	case secretspkg.KeyTypeRSA, secretspkg.KeyTypeECDSA:
		rsaKey, rsaErr := rsa.GenerateKey(rand.Reader, 2048)
		if rsaErr != nil {
			return nil, nil, nil, fmt.Errorf("generate RSA private key: %w", rsaErr)
		}
		privateKey = rsaKey
		privateKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)})
	case secretspkg.KeyTypeEd25519:
		_, edKey, edErr := ed25519.GenerateKey(rand.Reader)
		if edErr != nil {
			return nil, nil, nil, fmt.Errorf("generate Ed25519 private key: %w", edErr)
		}
		privateKey = edKey
		marshaled, marshalErr := x509.MarshalPKCS8PrivateKey(edKey)
		if marshalErr != nil {
			return nil, nil, nil, fmt.Errorf("marshal Ed25519 private key: %w", marshalErr)
		}
		privateKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: marshaled})
	default:
		return nil, nil, nil, fmt.Errorf("unsupported key type %d", keyType)
	}

	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, publicKey(privateKey), privateKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create self-signed certificate: %w", err)
	}

	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	pair, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse generated key pair: %w", err)
	}
	pair.Leaf, _ = x509.ParseCertificate(pair.Certificate[0])

	return certificatePEM, privateKeyPEM, &pair, nil
}

// publicKey extracts the corresponding public key from the generated private key.
func publicKey(privateKey any) any {
	switch key := privateKey.(type) {
	case *rsa.PrivateKey:
		return &key.PublicKey
	case ed25519.PrivateKey:
		return key.Public()
	default:
		return nil
	}
}
