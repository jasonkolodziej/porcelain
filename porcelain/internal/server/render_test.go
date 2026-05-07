package server_test

// Tier-2 render tests use goquery to make HTML-structure assertions on the
// responses from the live mTLS test server. Every test in this file follows
// the same pattern:
//
//  1. Spin up the test server with testpki.New.
//  2. Fetch a page with a valid mTLS client.
//  3. Parse the response body with goquery.
//  4. Assert on element text, attributes, or presence.
//
// These tests run alongside router_e2e_test.go and share its helpers
// (startMTLSServer, newClient, mustGet).
//
// Add a new sub-test for every page or component that ships HTML-rendered
// content. The convention is:
//
//   TestPageRender_<PageName>    – breadcrumb, heading, key copy
//   TestPageRender_ModuleHealth  – module health table across pages
//   TestPageRender_HTMX          – HTMX attribute wiring (targets, headers)
//   TestPageRender_XPageTitle    – X-Page-Title header for breadcrumb sync

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/auth"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/config"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/server"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/testpki"
)

// newRenderTestServer is the shared setup for all render tests. It always
// runs with a nil registry so modules degrade gracefully to stubs.
func newRenderTestServer(t *testing.T) (addr string, shutdown func(), client *http.Client) {
	t.Helper()

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

	addr, shutdown = startMTLSServer(t, app, bundle)
	client = newClient(bundle.ClientTLSConfig())
	return addr, shutdown, client
}

// parseBody reads the response body and parses it with goquery. It does NOT
// close the response body; callers should defer resp.Body.Close() before
// calling parseBody.
func parseBody(t *testing.T, resp *http.Response) *goquery.Document {
	t.Helper()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		t.Fatalf("goquery parse: %v", err)
	}
	return doc
}

// --- Breadcrumb helpers --------------------------------------------------

// assertBreadcrumb checks that the top-bar breadcrumb span contains want.
// The span is the last <span> inside the breadcrumb wrapper and carries
// both a static {{ .Title }} value (for initial render) and x-text="breadcrumb"
// (for HTMX partial updates via Alpine).
func assertBreadcrumb(t *testing.T, doc *goquery.Document, want string) {
	t.Helper()

	// The breadcrumb <span> is the third element inside the breadcrumb
	// wrapper: "Porcelain" / chevron / title.
	got := strings.TrimSpace(doc.Find("header span[x-text]").Text())
	if got != want {
		t.Fatalf("breadcrumb: got %q, want %q", got, want)
	}
}

// assertXPageTitle checks the X-Page-Title response header. This header is
// consumed by the HTMX afterSwap handler to update the Alpine breadcrumb
// state without a full-page reload.
func assertXPageTitle(t *testing.T, resp *http.Response, want string) {
	t.Helper()

	got := resp.Header.Get("X-Page-Title")
	if got != want {
		t.Fatalf("X-Page-Title: got %q, want %q", got, want)
	}
}

// --- Heading helpers -----------------------------------------------------

// assertH1 checks that the page renders an <h1> containing want.
func assertH1(t *testing.T, doc *goquery.Document, want string) {
	t.Helper()

	got := strings.TrimSpace(doc.Find("h1").First().Text())
	if got != want {
		t.Fatalf("<h1>: got %q, want %q", got, want)
	}
}

// assertLeadText checks that the page renders a <p> containing want as its
// lead/description copy (immediately after the <h1> in the page header).
func assertLeadText(t *testing.T, doc *goquery.Document, want string) {
	t.Helper()

	found := false
	doc.Find("p").Each(func(_ int, s *goquery.Selection) {
		if strings.Contains(s.Text(), want) {
			found = true
		}
	})
	if !found {
		t.Fatalf("lead text %q not found in any <p>", want)
	}
}

// --- HTMX wiring helpers -------------------------------------------------

// assertHTMXTarget checks that at least one element on the page has the
// given hx-target value, confirming HTMX partial swap wiring is in place.
func assertHTMXTarget(t *testing.T, doc *goquery.Document, target string) {
	t.Helper()

	count := doc.Find("[hx-target]").FilterFunction(func(_ int, s *goquery.Selection) bool {
		v, _ := s.Attr("hx-target")
		return v == target
	}).Length()
	if count == 0 {
		t.Fatalf("no element found with hx-target=%q", target)
	}
}

// assertHTMXGet checks that the page has an hx-get link pointing to path.
func assertHTMXGet(t *testing.T, doc *goquery.Document, path string) {
	t.Helper()

	count := doc.Find("[hx-get]").FilterFunction(func(_ int, s *goquery.Selection) bool {
		v, _ := s.Attr("hx-get")
		return v == path
	}).Length()
	if count == 0 {
		t.Fatalf("no element found with hx-get=%q", path)
	}
}

// --- Module sidebar helpers ----------------------------------------------

// assertSidebarLink checks that the sidebar contains an <a> linking to path
// with the given display text.
func assertSidebarLink(t *testing.T, doc *goquery.Document, path, text string) {
	t.Helper()

	found := false
	doc.Find("nav a[href]").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if href == path && strings.Contains(s.Text(), text) {
			found = true
		}
	})
	if !found {
		t.Fatalf("sidebar: no link to %q with text %q", path, text)
	}
}

// --- User info helper ----------------------------------------------------

// assertUserBar checks that the top-bar user menu shows the expected name.
func assertUserBar(t *testing.T, doc *goquery.Document, name string) {
	t.Helper()

	found := false
	doc.Find("header button").Each(func(_ int, s *goquery.Selection) {
		if strings.Contains(s.Text(), name) {
			found = true
		}
	})
	if !found {
		t.Fatalf("user bar: name %q not found in any header button", name)
	}
}

// =========================================================================
// TestPageRender_XPageTitle ensures every page route emits the header that
// the HTMX afterSwap handler reads to update the Alpine breadcrumb state.
// =========================================================================

func TestPageRender_XPageTitle(t *testing.T) {
	addr, shutdown, client := newRenderTestServer(t)
	defer shutdown()

	cases := []struct {
		path  string
		title string
	}{
		{"/", "Dashboard"},
		{"/storage", "Storage"},
		{"/storage/zfs", "ZFS"},
		{"/network", "Network"},
		{"/network/firewall", "Firewall"},
		{"/network/cloudflare", "Cloudflare"},
		{"/podman", "Containers"},
		{"/diagnostics", "Diagnostics"},
		{"/sensors", "Sensors"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			resp := mustGet(t, client, "https://"+addr+tc.path)
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status: %d", resp.StatusCode)
			}
			assertXPageTitle(t, resp, tc.title)
		})
	}
}

// =========================================================================
// TestPageRender_BreadcrumbAndTitle verifies that the breadcrumb span and
// the <h1> on each page carry the correct, page-specific titles.
// =========================================================================

func TestPageRender_BreadcrumbAndTitle(t *testing.T) {
	addr, shutdown, client := newRenderTestServer(t)
	defer shutdown()

	cases := []struct {
		path  string
		title string // breadcrumb and <title> tag value
		h1    string // first <h1> on the page
	}{
		{"/", "Dashboard", "Dashboard"},
		{"/network", "Network", "Network"},
		{"/network/firewall", "Firewall", "Firewall"},
		{"/network/cloudflare", "Cloudflare", "Cloudflare"},
		{"/podman", "Containers", "Containers"},
		{"/diagnostics", "Diagnostics", "Diagnostics"},
		{"/sensors", "Sensors", "Sensors"},
		{"/storage", "Storage", "Storage"},
		{"/storage/zfs", "ZFS", "ZFS"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			resp := mustGet(t, client, "https://"+addr+tc.path)
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d for %s", resp.StatusCode, tc.path)
			}

			doc := parseBody(t, resp)

			// <title> tag
			pageTitle := doc.Find("title").Text()
			if !strings.Contains(pageTitle, tc.title) {
				t.Fatalf("<title>: got %q, want it to contain %q", pageTitle, tc.title)
			}

			// breadcrumb span (static initial-render value)
			assertBreadcrumb(t, doc, tc.title)

			// <h1> heading
			assertH1(t, doc, tc.h1)
		})
	}
}

// =========================================================================
// TestPageRender_HTMLDocument ensures every page emits a well-formed HTML5
// skeleton: <html>, <head>, <body>, exactly one <h1>, and the shared base
// classes on <body>.
// =========================================================================

func TestPageRender_HTMLDocument(t *testing.T) {
	addr, shutdown, client := newRenderTestServer(t)
	defer shutdown()

	paths := []string{"/", "/network", "/network/firewall", "/network/cloudflare", "/diagnostics", "/podman", "/storage/zfs"}

	for _, path := range paths {
		path := path
		t.Run(path, func(t *testing.T) {
			resp := mustGet(t, client, "https://"+addr+path)
			defer resp.Body.Close()

			doc := parseBody(t, resp)

			// Must have one <html>, one <head>, one <body>.
			if doc.Find("html").Length() != 1 {
				t.Fatal("missing <html>")
			}
			if doc.Find("head").Length() != 1 {
				t.Fatal("missing <head>")
			}
			if doc.Find("body").Length() != 1 {
				t.Fatal("missing <body>")
			}

			// Exactly one <h1> per page.
			if n := doc.Find("h1").Length(); n != 1 {
				t.Fatalf("expected 1 <h1>, got %d", n)
			}

			// Base CSS class applied by dark-mode init.
			bodyClass, _ := doc.Find("body").Attr("class")
			if !strings.Contains(bodyClass, "font-sans") {
				t.Fatalf("<body> class missing font-sans: %q", bodyClass)
			}
		})
	}
}

// =========================================================================
// TestPageRender_SidebarWiring checks that every page includes the expected
// sidebar navigation links and HTMX partial-swap wiring.
// =========================================================================

func TestPageRender_SidebarWiring(t *testing.T) {
	addr, shutdown, client := newRenderTestServer(t)
	defer shutdown()

	resp := mustGet(t, client, "https://"+addr+"/")
	defer resp.Body.Close()

	doc := parseBody(t, resp)

	links := []struct{ path, text string }{
		{"/", "Dashboard"},
		{"/storage", "Storage"},
		{"/network", "Network"},
		{"/podman", "Containers"},
		{"/diagnostics", "Diagnostics"},
		{"/sensors", "Sensors"},
	}
	for _, l := range links {
		assertSidebarLink(t, doc, l.path, l.text)
	}

	// All nav links must target #main-content for HTMX partial swaps.
	assertHTMXTarget(t, doc, "#main-content")

	// Each nav link must have hx-push-url so the browser URL updates.
	count := doc.Find("nav a[hx-push-url='true']").Length()
	if count == 0 {
		t.Fatal("no nav links with hx-push-url='true'")
	}
}

// =========================================================================
// TestPageRender_NetworkPage validates Phase 3 network page UI elements.
// =========================================================================

func TestPageRender_NetworkPage(t *testing.T) {
	addr, shutdown, client := newRenderTestServer(t)
	defer shutdown()

	resp := mustGet(t, client, "https://"+addr+"/network")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	assertXPageTitle(t, resp, "Network")

	doc := parseBody(t, resp)

	assertH1(t, doc, "Network")
	assertLeadText(t, doc, "NetworkManager")
	assertBreadcrumb(t, doc, "Network")

	// Enable/Disable buttons must be present (Phase 3 write controls).
	enableBtn := doc.Find("button").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return strings.Contains(s.Text(), "Enable")
	})
	if enableBtn.Length() == 0 {
		t.Fatal("network page: Enable button not found")
	}
	disableBtn := doc.Find("button").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return strings.Contains(s.Text(), "Disable")
	})
	if disableBtn.Length() == 0 {
		t.Fatal("network page: Disable button not found")
	}
}

// =========================================================================
// TestPageRender_FirewallPage validates Phase 3 firewall page UI elements.
// =========================================================================

func TestPageRender_FirewallPage(t *testing.T) {
	addr, shutdown, client := newRenderTestServer(t)
	defer shutdown()

	resp := mustGet(t, client, "https://"+addr+"/network/firewall")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	assertXPageTitle(t, resp, "Firewall")

	doc := parseBody(t, resp)

	assertH1(t, doc, "Firewall")
	assertLeadText(t, doc, "firewalld")
	assertBreadcrumb(t, doc, "Firewall")
}

// =========================================================================
// TestPageRender_ZFSPage validates the ZFS page heading and empty-state UI.
// =========================================================================

func TestPageRender_ZFSPage(t *testing.T) {
	addr, shutdown, client := newRenderTestServer(t)
	defer shutdown()

	resp := mustGet(t, client, "https://"+addr+"/storage/zfs")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	assertXPageTitle(t, resp, "ZFS")

	doc := parseBody(t, resp)

	assertH1(t, doc, "ZFS")
	assertLeadText(t, doc, "Pool health, dataset usage, and scrub status")
	assertBreadcrumb(t, doc, "ZFS")

	bodyText := strings.TrimSpace(doc.Find("body").Text())
	if !strings.Contains(bodyText, "No ZFS pools found") {
		t.Fatal("zfs page: empty state not found")
	}
}

// =========================================================================
// TestPageRender_CloudflarePage validates the Cloudflare page empty state.
// =========================================================================

func TestPageRender_CloudflarePage(t *testing.T) {
	addr, shutdown, client := newRenderTestServer(t)
	defer shutdown()

	resp := mustGet(t, client, "https://"+addr+"/network/cloudflare")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	assertXPageTitle(t, resp, "Cloudflare")

	doc := parseBody(t, resp)

	assertH1(t, doc, "Cloudflare")
	assertLeadText(t, doc, "cloudflared tunnel agent and active tunnels")
	assertBreadcrumb(t, doc, "Cloudflare")

	bodyText := strings.TrimSpace(doc.Find("body").Text())
	if !strings.Contains(bodyText, "No tunnels registered.") {
		t.Fatal("cloudflare page: empty state not found")
	}
}

// =========================================================================
// TestPageRender_PodmanLogsDrawer validates the HTMX log drawer shell.
// =========================================================================

func TestPageRender_PodmanLogsDrawer(t *testing.T) {
	addr, shutdown, client := newRenderTestServer(t)
	defer shutdown()

	resp := mustGet(t, client, "https://"+addr+"/podman")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	doc := parseBody(t, resp)

	if doc.Find("#log-output").Length() != 1 {
		t.Fatal("podman page: log drawer content target not found")
	}

	bodyText := strings.TrimSpace(doc.Find("body").Text())
	if !strings.Contains(bodyText, "Select a container to view logs.") {
		t.Fatal("podman page: initial log drawer helper text not found")
	}
}

// =========================================================================
// TestPageRender_DiagnosticsPage validates the diagnostics page elements.
// =========================================================================

func TestPageRender_DiagnosticsPage(t *testing.T) {
	addr, shutdown, client := newRenderTestServer(t)
	defer shutdown()

	resp := mustGet(t, client, "https://"+addr+"/diagnostics")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	assertXPageTitle(t, resp, "Diagnostics")

	doc := parseBody(t, resp)

	assertH1(t, doc, "Diagnostics")
	assertBreadcrumb(t, doc, "Diagnostics")

	// Download Bundle link must be present.
	bundleLink := doc.Find("a[href='/api/diagnostics/bundle']")
	if bundleLink.Length() == 0 {
		t.Fatal("diagnostics page: Download Bundle link not found")
	}

	// Module Health section must be present.
	body, _ := io.ReadAll(strings.NewReader(doc.Find("body").Text()))
	if !strings.Contains(string(body), "Module Health") {
		t.Fatal("diagnostics page: Module Health section not found")
	}
}

// =========================================================================
// TestPageRender_HTMXPartialSwap confirms that HTMX partial requests (with
// the HX-Request header set) return only the content block, not a full page,
// while the X-Page-Title header is still set.
// =========================================================================

func TestPageRender_HTMXPartialSwap(t *testing.T) {
	addr, shutdown, client := newRenderTestServer(t)
	defer shutdown()

	cases := []struct {
		path  string
		title string
	}{
		{"/network", "Network"},
		{"/network/firewall", "Firewall"},
		{"/network/cloudflare", "Cloudflare"},
		{"/diagnostics", "Diagnostics"},
		{"/podman", "Containers"},
		{"/storage/zfs", "ZFS"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "https://"+addr+tc.path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			// Simulate HTMX partial swap request.
			req.Header.Set("HX-Request", "true")

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status: %d", resp.StatusCode)
			}

			// X-Page-Title must be set even on partial responses.
			assertXPageTitle(t, resp, tc.title)

			body, _ := io.ReadAll(resp.Body)
			html := string(body)

			// Partial must include the page heading.
			if !strings.Contains(html, tc.title) {
				t.Fatalf("partial missing title %q", tc.title)
			}

			// Partial must NOT include the <html> or <head> tags (no full page).
			if strings.Contains(html, "<html") {
				t.Fatal("HTMX partial should not contain <html> tag")
			}
			if strings.Contains(html, "<head>") {
				t.Fatal("HTMX partial should not contain <head> tag")
			}
		})
	}
}

func TestPodmanLogsAPI_RegistryUnavailable(t *testing.T) {
	addr, shutdown, client := newRenderTestServer(t)
	defer shutdown()

	resp := mustGet(t, client, "https://"+addr+"/api/podman/containers/example/logs?tail=100")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "module registry unavailable") {
		t.Fatalf("body: got %q", string(body))
	}
}
