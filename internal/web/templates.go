package web

import (
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"
)

// Templates manages HTML template rendering.
type Templates struct {
	templates map[string]*template.Template
}

// NewTemplates parses all templates from the embedded filesystem.
// Each page gets its own template instance with layouts and partials included.
// Partials are also registered for direct rendering (htmx responses).
func NewTemplates(fsys fs.FS) (*Templates, error) {
	funcs := template.FuncMap{
		"safeJS": func(s string) template.JS {
			return template.JS(s) // #nosec G203 -- server-generated JSON
		},
		"add": func(a int, b int) int {
			return a + b
		},
		"hasPrefix": func(s, prefix string) bool {
			return strings.HasPrefix(s, prefix)
		},
		"formatDuration": func(ms int64) string {
			if ms <= 0 {
				return ""
			}
			d := time.Duration(ms) * time.Millisecond
			if d < time.Second {
				return fmt.Sprintf("%dms", ms)
			}
			if d < 10*time.Second {
				return fmt.Sprintf("%.1fs", float64(d)/float64(time.Second))
			}
			if d < time.Minute {
				return fmt.Sprintf("%ds", int(d.Seconds()+0.5))
			}
			mins := int(d.Minutes())
			secs := int(d.Seconds()) % 60
			return fmt.Sprintf("%dm %ds", mins, secs)
		},
		"formatCompact": func(value int) string {
			if value < 0 {
				return fmt.Sprintf("-%s", formatCompactNumber(-value))
			}
			return formatCompactNumber(value)
		},
		"formatBytes": func(bytes int64) string {
			return formatBytesNumber(bytes)
		},
		"divideFloat": func(a, b int64) float64 {
			if b == 0 {
				return 0
			}
			return float64(a) / float64(b)
		},
		"mulFloat": func(a float64, b float64) float64 {
			return a * b
		},
		"int": func(v int64) int {
			return int(v)
		},
		"formatNumber": func(n int) string {
			return formatNumberWithCommas(n)
		},
		"timeAgo": func(t time.Time) string {
			now := time.Now()
			today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			if t.Before(today) {
				return t.Format("2006-01-02")
			}
			d := now.Sub(t)
			switch {
			case d < time.Minute:
				return "just now"
			case d < time.Hour:
				mins := int(d.Minutes())
				if mins == 1 {
					return "1 minute ago"
				}
				return fmt.Sprintf("%d minutes ago", mins)
			default:
				hours := int(d.Hours())
				if hours == 1 {
					return "1 hour ago"
				}
				return fmt.Sprintf("%d hours ago", hours)
			}
		},
		"formatInstalls": func(n int) string {
			switch {
			case n >= 1_000_000_000:
				return fmt.Sprintf("%.0fB", float64(n)/1_000_000_000)
			case n >= 1_000_000:
				return fmt.Sprintf("%.0fM", float64(n)/1_000_000)
			case n >= 10_000:
				return fmt.Sprintf("%.0fK", float64(n)/1_000)
			default:
				return formatNumberWithCommas(n)
			}
		},
		"percentage": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a * 100 / b
		},
	}

	// Collect shared template patterns (layouts and partials)
	sharedPatterns := []string{"layouts/*.html", "partials/*.html"}

	// Get all page templates
	pages, err := fs.Glob(fsys, "pages/*.html")
	if err != nil {
		return nil, err
	}

	// Get partial names for direct rendering
	partials, err := fs.Glob(fsys, "partials/*.html")
	if err != nil {
		return nil, err
	}

	t := &Templates{
		templates: make(map[string]*template.Template),
	}

	// Parse each page with its own copy of shared templates + the page itself
	for _, page := range pages {
		name := path.Base(page)
		patterns := append(sharedPatterns, page)

		tmpl, err := template.New(name).Funcs(funcs).ParseFS(fsys, patterns...)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", name, err)
		}

		t.templates[name] = tmpl
	}

	// Also register partials for direct rendering (htmx responses)
	for _, partial := range partials {
		name := path.Base(partial)
		tmpl, err := template.New(name).Funcs(funcs).ParseFS(fsys, partial)
		if err != nil {
			return nil, fmt.Errorf("parsing partial %s: %w", name, err)
		}
		t.templates[name] = tmpl
	}

	return t, nil
}

// Render executes the named template with the given data.
func (t *Templates) Render(w io.Writer, name string, data any) error {
	tmpl, ok := t.templates[name]
	if !ok {
		return fmt.Errorf("template %q not found", name)
	}
	return tmpl.ExecuteTemplate(w, name, data)
}

func formatCompactNumber(value int) string {
	switch {
	case value >= 1_000_000_000:
		return fmt.Sprintf("%.0fB", float64(value)/1_000_000_000)
	case value >= 1_000_000:
		return fmt.Sprintf("%.0fM", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.0fK", float64(value)/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}

func formatBytesNumber(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func formatNumberWithCommas(n int) string {
	if n < 0 {
		return "-" + formatNumberWithCommas(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return formatNumberWithCommas(n/1000) + "," + fmt.Sprintf("%03d", n%1000)
}
