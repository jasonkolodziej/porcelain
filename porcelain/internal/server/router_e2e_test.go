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
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules/zfs"
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

	t.Run("anonymous handshake is rejected", func(t *testing.T) { assertAnonymousHandshakeRejected(t, bundle, addr) })
	t.Run("authenticated /healthz returns ok", func(t *testing.T) { assertHealthzOK(t, bundle, addr) })
	t.Run("authenticated /api/modules returns the module catalogue", func(t *testing.T) {
		assertAPIModules(t, bundle, addr)
	})
	t.Run("authenticated /api/diagnostics/bundle returns attachment", func(t *testing.T) {
		assertDiagnosticsBundle(t, bundle, addr)
	})
	t.Run("dashboard surfaces the verified peer common name", func(t *testing.T) {
		assertDashboardIncludesPeer(t, bundle, addr)
	})
	t.Run("network page renders first-class phase 3 UI", func(t *testing.T) {
		assertNetworkPageUI(t, bundle, addr)
	})
	t.Run("firewall page renders first-class phase 3 UI", func(t *testing.T) {
		assertFirewallPageUI(t, bundle, addr)
	})
}

func assertAnonymousHandshakeRejected(t *testing.T, bundle *testpki.Bundle, addr string) {
	t.Helper()
	client := newClient(bundle.AnonymousClientTLSConfig())
	_, err := client.Get("https://" + addr + "/healthz")
	if err == nil {
		t.Fatalf("expected handshake failure without client certificate")
	}
}

func assertHealthzOK(t *testing.T, bundle *testpki.Bundle, addr string) {
	t.Helper()
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
}

func assertAPIModules(t *testing.T, bundle *testpki.Bundle, addr string) {
	t.Helper()
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
}

func assertDiagnosticsBundle(t *testing.T, bundle *testpki.Bundle, addr string) {
	t.Helper()
	client := newClient(bundle.ClientTLSConfig())
	resp := mustGet(t, client, "https://"+addr+"/api/diagnostics/bundle")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "attachment;") {
		t.Fatalf("content-disposition: %q", got)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "application/gzip") {
		t.Fatalf("content-type: %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(body) == 0 {
		t.Fatalf("expected non-empty bundle body")
	}
}

func assertDashboardIncludesPeer(t *testing.T, bundle *testpki.Bundle, addr string) {
	t.Helper()
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
}

func assertNetworkPageUI(t *testing.T, bundle *testpki.Bundle, addr string) {
	t.Helper()
	client := newClient(bundle.ClientTLSConfig())
	resp := mustGet(t, client, "https://"+addr+"/network")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !strings.Contains(text, "Interface and connectivity controls via NetworkManager") {
		t.Fatalf("network page missing first-class heading content")
	}
}

func assertFirewallPageUI(t *testing.T, bundle *testpki.Bundle, addr string) {
	t.Helper()
	client := newClient(bundle.ClientTLSConfig())
	resp := mustGet(t, client, "https://"+addr+"/network/firewall")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !strings.Contains(text, "Zone and service controls via firewalld") {
		t.Fatalf("firewall page missing first-class heading content")
	}
}

func TestPolicyGatedWriteRoutesWithDexEnabled(t *testing.T) {
	bundle := testpki.New(t, "operator@example.com")

	cfg := config.Config{
		Server:       config.ServerConfig{Address: "127.0.0.1:0"},
		Auth:         config.AuthConfig{Enabled: true, AdminGroup: "porcelain-admins"},
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

	t.Run("write rejected without bearer token", func(t *testing.T) {
		client := newClient(bundle.ClientTLSConfig())
		req, err := http.NewRequest(http.MethodPost, "https://"+addr+"/api/network/enabled/true", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status: %d", resp.StatusCode)
		}
	})

	t.Run("write passes policy for admin group and reaches module gate", func(t *testing.T) {
		client := newClient(bundle.ClientTLSConfig())
		req, err := http.NewRequest(http.MethodPost, "https://"+addr+"/api/network/enabled/true", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("X-Porcelain-Subject", "alice")
		req.Header.Set("X-Porcelain-Groups", "porcelain-admins")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer resp.Body.Close()

		// Registry is nil in this test; reaching this 503 means policy allowed.
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status: %d", resp.StatusCode)
		}
	})

	t.Run("sensors threshold write rejected without bearer token", func(t *testing.T) {
		client := newClient(bundle.ClientTLSConfig())
		req, err := http.NewRequest(http.MethodPost, "https://"+addr+"/api/sensors/thresholds", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status: %d", resp.StatusCode)
		}
	})

	t.Run("sensors threshold routes pass policy for admin group and reach module gate", func(t *testing.T) {
		client := newClient(bundle.ClientTLSConfig())
		for _, tc := range []struct {
			method string
			url    string
		}{
			{http.MethodPost, "https://" + addr + "/api/sensors/thresholds"},
			{http.MethodDelete, "https://" + addr + "/api/sensors/thresholds?chip=chip0&reading=fan1"},
			{http.MethodPost, "https://" + addr + "/api/sensors/thresholds/clear"},
		} {
			req, err := http.NewRequest(tc.method, tc.url, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("Authorization", "Bearer test-token")
			req.Header.Set("X-Porcelain-Subject", "alice")
			req.Header.Set("X-Porcelain-Groups", "porcelain-admins")

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("%s %s: status %d", tc.method, tc.url, resp.StatusCode)
			}
		}
	})
}

func TestZFSEventStream(t *testing.T) {
	bundle := testpki.New(t, "operator@example.com")

	cfg := config.Config{
		Server:       config.ServerConfig{Address: "127.0.0.1:0"},
		Auth:         config.AuthConfig{Enabled: false},
		DBus:         config.DBusConfig{Bus: "system", Optional: true},
		Secrets:      config.SecretsConfig{Backend: "memory"},
		Certificates: config.CertificatesConfig{Backend: "selfsigned", CommonName: "localhost"},
	}

	registry := modules.NewRegistry()
	registry.Register(zfs.New(context.Background()))

	dexAuth := auth.NewDexAuth(cfg.Auth, nil)
	app, err := server.NewRouter(cfg, dexAuth, registry)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	addr, shutdown := startMTLSServer(t, app, bundle)
	defer shutdown()

	client := newClient(bundle.ClientTLSConfig())
	req, err := http.NewRequest(http.MethodGet, "https://"+addr+"/api/zfs/stream?eventLimit=5&taskLimit=5", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content-type: %q", got)
	}

	buf := make([]byte, 1024)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read stream: %v", err)
	}
	chunk := string(buf[:n])
	if !strings.Contains(chunk, "event: events") {
		t.Fatalf("stream missing events frame: %q", chunk)
	}
	if !strings.Contains(chunk, "event: tasks") {
		t.Fatalf("stream missing tasks frame: %q", chunk)
	}
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
