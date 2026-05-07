package server_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/auth"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/config"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/server"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/testpki"
	"github.com/jasonkolodziej/porcelain/porcelain/pkg/api"
)

// TestEndToEndMTLS exercises the Fiber router behind a real mTLS listener so
// the middleware chain (peer identity extraction, Dex claims plumbing, route
// handlers, and template rendering) is validated together.
func TestEndToEndMTLS(t *testing.T) {
	bundle := testpki.New(t, "operator@example.com")

	cfg := config.Config{
		Server:       config.ServerConfig{Address: "127.0.0.1:0"},
		Auth:         config.AuthConfig{Enabled: false},
		DBus:         config.DBusConfig{Bus: "system", Optional: true},
		Secrets:      config.SecretsConfig{Backend: "memory"},
		Certificates: config.CertificatesConfig{Backend: "selfsigned", CommonName: "localhost"},
	}

	dexAuth := auth.NewDexAuth(cfg.Auth, nil)
	app, err := server.NewRouter(cfg, dexAuth, nil)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	addr, shutdown := startMTLSServer(t, app, bundle)
	defer shutdown()

	t.Run("anonymous handshake is rejected", func(t *testing.T) {
		client := newClient(bundle.AnonymousClientTLSConfig())
		_, err := client.Get("https://" + addr + "/healthz")
		if err == nil {
			t.Fatalf("expected handshake failure without client certificate")
		}
	})

	t.Run("authenticated /healthz returns ok", func(t *testing.T) {
		client := newClient(bundle.ClientTLSConfig())
		resp := mustGet(t, client, "https://"+addr+"/healthz")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "ok" {
			t.Fatalf("body: %q", body)
		}
		if got := resp.Header.Get("X-Porcelain-Peer-CN"); got != bundle.ClientCN {
			t.Fatalf("peer header: %q want %q", got, bundle.ClientCN)
		}
	})

	t.Run("authenticated /api/modules returns the module catalogue", func(t *testing.T) {
		client := newClient(bundle.ClientTLSConfig())
		resp := mustGet(t, client, "https://"+addr+"/api/modules")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: %d", resp.StatusCode)
		}

		var modules []api.ModuleView
		if err := json.NewDecoder(resp.Body).Decode(&modules); err != nil {
			t.Fatalf("decode modules: %v", err)
		}
		if len(modules) == 0 {
			t.Fatalf("expected at least one module card")
		}
		if !containsModule(modules, "Storage") {
			t.Fatalf("expected Storage module card, got %v", moduleNames(modules))
		}
	})

	t.Run("dashboard surfaces the verified peer common name", func(t *testing.T) {
		client := newClient(bundle.ClientTLSConfig())
		resp := mustGet(t, client, "https://"+addr+"/")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), bundle.ClientCN) {
			t.Fatalf("dashboard did not include peer CN %q", bundle.ClientCN)
		}
		if !strings.Contains(string(body), "Porcelain") {
			t.Fatalf("dashboard did not include site title")
		}
	})
}

// startMTLSServer wires the Fiber app behind a TLS listener that requires a
// verified client certificate from the test bundle.
func startMTLSServer(t *testing.T, app *fiber.App, bundle *testpki.Bundle) (string, func()) {
	t.Helper()

	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	tlsListener := tls.NewListener(tcpListener, bundle.ServerTLSConfig())

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- app.Listener(tlsListener, fiber.ListenConfig{DisableStartupMessage: true})
	}()

	// Probe until the listener accepts TLS connections so test cases see a
	// fully ready server instead of a race against startup.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := tls.Dial("tcp", tcpListener.Addr().String(), bundle.ClientTLSConfig())
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	shutdown := func() {
		_ = app.ShutdownWithContext(context.Background())
		select {
		case <-serveErr:
		case <-time.After(2 * time.Second):
		}
	}
	return tcpListener.Addr().String(), shutdown
}

func newClient(tlsCfg *tls.Config) *http.Client {
	return &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}
}

func mustGet(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func containsModule(modules []api.ModuleView, name string) bool {
	for _, m := range modules {
		if m.Name == name {
			return true
		}
	}
	return false
}

func moduleNames(modules []api.ModuleView) []string {
	names := make([]string, 0, len(modules))
	for _, m := range modules {
		names = append(names, m.Name)
	}
	return names
}
