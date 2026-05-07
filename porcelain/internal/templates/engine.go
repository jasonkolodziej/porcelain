// Package templates owns the embedded HTML templates, the static asset bundle,
// and the rendering engine that the Fiber router uses.
//
// The engine builds one parsed *template.Template per page so each page can
// re-define {{ define "content" }} without colliding with sibling pages.
package templates

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"path"
	"strings"
)

//go:embed all:*.html modules/*.html static
var assetFS embed.FS

// pageEntry maps a logical page name (e.g. "dashboard") to the concrete
// template file the engine should render alongside base.html.
type pageEntry struct {
	name string
	file string
}

// pages enumerates every renderable page. Adding a new page is a one-line
// change here once its template defines {{ define "content" }}.
var pages = []pageEntry{
	{name: "dashboard", file: "dashboard.html"},
	{name: "storage", file: "modules/storage.html"},
}

// Engine renders pages with a shared base layout and exposes the embedded
// static assets so Fiber can serve them under /static/*.
type Engine struct {
	pages map[string]*template.Template
}

// NewEngine parses every enumerated page against the shared base.html so
// callers can render by logical name.
func NewEngine() (*Engine, error) {
	baseRaw, err := assetFS.ReadFile("base.html")
	if err != nil {
		return nil, fmt.Errorf("read base.html: %w", err)
	}

	parsed := make(map[string]*template.Template, len(pages))
	for _, p := range pages {
		body, err := assetFS.ReadFile(p.file)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p.file, err)
		}

		tmpl := template.New(path.Base(p.file)).Funcs(funcMap)
		if _, err := tmpl.Parse(string(baseRaw)); err != nil {
			return nil, fmt.Errorf("parse base for %s: %w", p.name, err)
		}
		if _, err := tmpl.Parse(string(body)); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p.name, err)
		}
		parsed[p.name] = tmpl
	}

	return &Engine{pages: parsed}, nil
}

// Render writes the named page through the shared base layout into w.
func (e *Engine) Render(w io.Writer, page string, data TemplateData) error {
	tmpl, ok := e.pages[page]
	if !ok {
		return fmt.Errorf("unknown page %q", page)
	}
	return tmpl.ExecuteTemplate(w, "base", data)
}

// RenderPartial writes only the {{ define "content" }} block, which is what
// HTMX swaps target when navigating in-place.
func (e *Engine) RenderPartial(w io.Writer, page string, data TemplateData) error {
	tmpl, ok := e.pages[page]
	if !ok {
		return fmt.Errorf("unknown page %q", page)
	}
	return tmpl.ExecuteTemplate(w, "content", data)
}

// StaticFS exposes the embedded /static tree so the HTTP layer can serve
// CSS, JS, and image assets without re-embedding them.
func (e *Engine) StaticFS() fs.FS {
	sub, err := fs.Sub(assetFS, "static")
	if err != nil {
		// embed guarantees the directory exists; treat any error as fatal.
		panic(fmt.Errorf("static sub-fs: %w", err))
	}
	return sub
}

// PageNames lists the logical page names registered with the engine. Useful
// for tests and diagnostics.
func (e *Engine) PageNames() []string {
	names := make([]string, 0, len(e.pages))
	for name := range e.pages {
		names = append(names, name)
	}
	return names
}

// HasPage reports whether the named page is registered.
func (e *Engine) HasPage(name string) bool {
	_, ok := e.pages[name]
	return ok
}

// stripDot is exposed so handler code can normalize the trailing-slash form
// HTMX may submit.
func stripDot(s string) string { return strings.TrimSuffix(s, ".") }
