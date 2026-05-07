package templates

import (
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"time"
)

// funcMap is shared across every parsed page so authors can rely on the same
// helpers in base.html, content blocks, and any partials.
var funcMap = template.FuncMap{
	"humanizeBytes":    humanizeBytes,
	"humanizeDuration": humanizeDuration,
	"formatTime": func(t time.Time, layout string) string {
		return t.Format(layout)
	},
	"json": func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	},
	"add": func(a, b int) int { return a + b },
	"sub": func(a, b int) int { return a - b },
	"mul": func(a, b int) int { return a * b },
	"div": func(a, b int) int {
		if b == 0 {
			return 0
		}
		return a / b
	},
	"mod": func(a, b int) int {
		if b == 0 {
			return 0
		}
		return a % b
	},
	"ternary": func(cond bool, a, b any) any {
		if cond {
			return a
		}
		return b
	},
	"slice": func(s string, start, end int) string {
		if start < 0 {
			start = 0
		}
		if end > len(s) {
			end = len(s)
		}
		if start > end {
			start = end
		}
		return s[start:end]
	},
	"lower":     strings.ToLower,
	"upper":     strings.ToUpper,
	"title":     strings.Title, //nolint:staticcheck // matching original handler API
	"contains":  strings.Contains,
	"hasPrefix": strings.HasPrefix,
	"hasSuffix": strings.HasSuffix,
	"replace":   strings.ReplaceAll,
	"split":     strings.Split,
	"join":      strings.Join,
	"truncate": func(s string, n int) string {
		if n <= 3 || len(s) <= n {
			return s
		}
		return s[:n-3] + "..."
	},
	"percentage": func(part, total int64) float64 {
		if total == 0 {
			return 0
		}
		return float64(part) / float64(total) * 100
	},
	"percentageString": func(part, total int64) string {
		if total == 0 {
			return "0.0"
		}
		return fmt.Sprintf("%.1f", float64(part)/float64(total)*100)
	},
	"safeHTML": func(s string) template.HTML { return template.HTML(s) },
	"safeJS":   func(s string) template.JS { return template.JS(s) },
}

// humanizeBytes renders a byte count as a human-friendly string.
func humanizeBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// humanizeDuration renders a duration as a compact string.
func humanizeDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
