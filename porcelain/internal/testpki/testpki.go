// Package testpki produces in-memory CAs, server certificates, and client
// certificates that share the same trust chain, so end-to-end tests can
// exercise the full mTLS handshake against the Fiber router.
package testpki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"testing"
	"time"
)

// Bundle carries everything an E2E test needs to drive both sides of an
// mTLS handshake.
type Bundle struct {
	CACert     *x509.Certificate
	CAPool     *x509.CertPool
	ServerCert tls.Certificate
	ClientCert tls.Certificate
	ClientCN   string
}

// New builds a fresh CA, leaf server certificate (with localhost + 127.0.0.1
// SANs), and a leaf client certificate (with the supplied common name).
//
// Failures use t.Fatalf so callers do not have to thread error returns.
func New(t *testing.T, clientCommonName string) *Bundle {
	t.Helper()

	caKey, caCert, caPEM := mustIssueCA(t)
	serverPair := mustIssueLeaf(t, caCert, caKey, leafSpec{
		commonName:  "porcelain-test-server",
		dnsNames:    []string{"localhost"},
		ipAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		isClient:    false,
	})
	clientPair := mustIssueLeaf(t, caCert, caKey, leafSpec{
		commonName: clientCommonName,
		isClient:   true,
	})

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatalf("append CA to pool")
	}

	return &Bundle{
		CACert:     caCert,
		CAPool:     pool,
		ServerCert: serverPair,
		ClientCert: clientPair,
		ClientCN:   clientCommonName,
	}
}

// ServerTLSConfig returns the tls.Config a Fiber listener should adopt to
// require and verify client certificates from this bundle.
func (b *Bundle) ServerTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{b.ServerCert},
		ClientCAs:    b.CAPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		NextProtos:   []string{"h2", "http/1.1"},
	}
}

// ClientTLSConfig returns a tls.Config that presents the bundle's client
// certificate and trusts the bundle's CA for the server.
func (b *Bundle) ClientTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		RootCAs:      b.CAPool,
		Certificates: []tls.Certificate{b.ClientCert},
		ServerName:   "localhost",
	}
}

// AnonymousClientTLSConfig returns a tls.Config that trusts the bundle's CA
// but presents no client certificate, so handshake failure can be asserted.
func (b *Bundle) AnonymousClientTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    b.CAPool,
		ServerName: "localhost",
	}
}

type leafSpec struct {
	commonName  string
	dnsNames    []string
	ipAddresses []net.IP
	isClient    bool
}

func mustIssueCA(t *testing.T) (*ecdsa.PrivateKey, *x509.Certificate, []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          mustSerial(t),
		Subject:               pkix.Name{CommonName: "porcelain-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return key, cert, pemBytes
}

func mustIssueLeaf(t *testing.T, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, spec leafSpec) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}

	usage := x509.ExtKeyUsageServerAuth
	if spec.isClient {
		usage = x509.ExtKeyUsageClientAuth
	}

	template := &x509.Certificate{
		SerialNumber: mustSerial(t),
		Subject:      pkix.Name{CommonName: spec.commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		DNSNames:     spec.dnsNames,
		IPAddresses:  spec.ipAddresses,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create leaf cert: %v", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal leaf key: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("assemble leaf pair: %v", err)
	}
	pair.Leaf, _ = x509.ParseCertificate(der)

	return pair
}

func mustSerial(t *testing.T) *big.Int {
	t.Helper()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	return serial
}

// String renders the bundle for debugging.
func (b *Bundle) String() string {
	return fmt.Sprintf("Bundle{client=%s issuer=%s}", b.ClientCN, b.CACert.Subject.CommonName)
}
