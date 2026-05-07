// Package middleware contains Fiber-aware request handlers that compose
// transport-level identity (mTLS) with application-level identity (Dex)
// so downstream handlers see a single, consistent subject.
package middleware

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// peerIdentityLocalsKey is the Fiber locals slot that carries the verified
// client certificate identity for the duration of the request.
const peerIdentityLocalsKey = "porcelain.peer_identity"

// PeerIdentity captures the subset of the verified client certificate that
// downstream handlers and audit code actually care about.
type PeerIdentity struct {
	// CommonName is the verified subject common name from the leaf certificate.
	CommonName string
	// Organizations are the leaf certificate's subject organization values.
	Organizations []string
	// DNSNames are the SAN DNS entries the peer presented.
	DNSNames []string
	// EmailAddresses are the SAN email entries the peer presented.
	EmailAddresses []string
	// SerialNumber is the leaf certificate's serial number rendered as a string.
	SerialNumber string
	// FingerprintSHA256 is the lowercase hex SHA-256 digest of the leaf DER bytes.
	FingerprintSHA256 string
	// IssuerCommonName captures the issuing CA so audit output reads cleanly.
	IssuerCommonName string
}

// MTLSPeerIdentity returns a Fiber middleware that converts the verified TLS
// peer chain into a structured identity stored on the request locals.
//
// It assumes the listener has already enforced
// tls.RequireAndVerifyClientCert; if no peer certificate is present the
// request is rejected with 401 so handlers never see an unauthenticated
// caller through this middleware.
func MTLSPeerIdentity() fiber.Handler {
	return func(c fiber.Ctx) error {
		tlsState := c.RequestCtx().TLSConnectionState()
		if tlsState == nil || len(tlsState.PeerCertificates) == 0 {
			return c.Status(fiber.StatusUnauthorized).
				SendString("missing client certificate")
		}

		leaf := tlsState.PeerCertificates[0]
		identity := newPeerIdentity(leaf)
		fiber.Locals[PeerIdentity](c, peerIdentityLocalsKey, identity)

		// Surface the verified peer to upstream proxies and audit logs in a
		// header that handlers can echo into structured logs without re-parsing
		// the certificate.
		c.Set("X-Porcelain-Peer-CN", identity.CommonName)
		c.Set("X-Porcelain-Peer-Fingerprint", identity.FingerprintSHA256)

		return c.Next()
	}
}

// PeerIdentityFromFiber returns the verified peer identity recorded by the
// middleware, or false if the request did not flow through it.
func PeerIdentityFromFiber(c fiber.Ctx) (PeerIdentity, bool) {
	identity, ok := c.Locals(peerIdentityLocalsKey).(PeerIdentity)
	if !ok {
		return PeerIdentity{}, false
	}

	return identity, true
}

// newPeerIdentity converts a verified leaf certificate into the structured
// identity stored on request locals.
func newPeerIdentity(leaf *x509.Certificate) PeerIdentity {
	digest := sha256.Sum256(leaf.Raw)

	return PeerIdentity{
		CommonName:        strings.TrimSpace(leaf.Subject.CommonName),
		Organizations:     append([]string(nil), leaf.Subject.Organization...),
		DNSNames:          append([]string(nil), leaf.DNSNames...),
		EmailAddresses:    append([]string(nil), leaf.EmailAddresses...),
		SerialNumber:      leaf.SerialNumber.String(),
		FingerprintSHA256: hex.EncodeToString(digest[:]),
		IssuerCommonName:  strings.TrimSpace(leaf.Issuer.CommonName),
	}
}
