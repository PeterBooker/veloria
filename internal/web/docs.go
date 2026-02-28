package web

import (
	"net/http"

	"veloria/internal/ui/page"
)

// DocsPage renders the documentation page.
func DocsPage(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pd := d.PageData(r)
		pd.OG.Title = "Documentation - Veloria"
		pd.OG.Description = "Learn how to search WordPress source code with regex patterns and integrate Veloria with AI tools via MCP."

		d.RenderComponent(w, r, page.Docs(pd))
	}
}
