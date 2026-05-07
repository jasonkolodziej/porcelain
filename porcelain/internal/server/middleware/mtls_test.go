package middleware_test

import (
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/server/middleware"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/testpki"
)

// TestMTLSPeerIdentityExtraction asserts the middleware promotes the verified
// leaf certificate into a structured identity and exposes it via Fiber locals
// and response headers.
func TestMTLSPeerIdentityExtraction(t *testing.T) {
	bundle := testpki.New(t, "alice@porcelain.test")

	app := fiber.New(fiber.Config{})
	app.Use(middleware.MTLSPeerIdentity())
	app.Get("/whoami", func(c fiber.Ctx) error {
		identity, ok := middleware.PeerIdentityFromFiber(c)
		if !ok {
			return c.Status(fiber.StatusInternalServerError).
				SendString("identity not on locals")
		}
		c.Type("json")
		return c.JSON(identity)
	})

	addr, shutdown := serveTLS(t, app, bundle)
	defer shutdown()

	client := &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{TLSClientConfig: bundle.ClientTLSConfig()},
	}

	resp, err := client.Get("https://" + addr + "/whoami")
	if err != nil {
		t.Fatalf("GET /whoami: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	var identity middleware.PeerIdentity
	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		t.Fatalf("decode identity: %v", err)
	}

	if identity.CommonName != bundle.ClientCN {
		t.Fatalf("common name: %q want %q", identity.CommonName, bundle.ClientCN)
	}
	if identity.IssuerCommonName != "porcelain-test-ca" {
		t.Fatalf("issuer: %q", identity.IssuerCommonName)
	}
	if len(identity.FingerprintSHA256) != 64 {
		t.Fatalf("fingerprint length: %d", len(identity.FingerprintSHA256))
	}
	if !strings.EqualFold(resp.Header.Get("X-Porcelain-Peer-CN"), bundle.ClientCN) {
		t.Fatalf("response header missing peer CN")
	}
}

// TestPeerIdentityFromFiberAbsent confirms callers receive a clean miss when
// the middleware has not run.
func TestPeerIdentityFromFiberAbsent(t *testing.T) {
	app := fiber.New(fiber.Config{})
	app.Get("/", func(c fiber.Ctx) error {
		if _, ok := middleware.PeerIdentityFromFiber(c); ok {
			return c.Status(fiber.StatusInternalServerError).SendString("unexpected identity")
		}
		return c.SendString("ok")
	})

	resp, err := app.Test(httpRequest(t, "GET", "/"))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

// httpRequest is a tiny helper so tests stay compact.
func httpRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, target, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return req
}

// serveTLS spins up a Fiber app on a random localhost port behind the test
// bundle's mTLS listener, waiting until the listener accepts traffic.
func serveTLS(t *testing.T, app *fiber.App, bundle *testpki.Bundle) (string, func()) {
	t.Helper()

	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	tlsListener := tls.NewListener(tcpListener, bundle.ServerTLSConfig())

	done := make(chan error, 1)
	go func() {
		done <- app.Listener(tlsListener, fiber.ListenConfig{DisableStartupMessage: true})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := tls.Dial("tcp", tcpListener.Addr().String(), bundle.ClientTLSConfig())
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	return tcpListener.Addr().String(), func() {
		_ = tcpListener.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}
