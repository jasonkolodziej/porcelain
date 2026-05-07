package templates

import (
	"embed"
	"html/template"
)

// files contains the HTML template assets compiled into the binary.
//
//go:embed *.html modules/*.html
var files embed.FS

// Parse loads the shared layout and module templates used by the scaffolded UI.
func Parse() (*template.Template, error) {
	return template.ParseFS(files, "*.html", "modules/*.html")
}
