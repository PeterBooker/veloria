package web

import "net/http"

// DocsPage renders the documentation page.
func DocsPage(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pd := d.PageData(r)
		pd.OG.Title = "Documentation - Veloria"
		pd.OG.Description = "Learn how to search WordPress source code with regex patterns and integrate Veloria with AI tools via MCP."

		data := struct{ PageData }{
			PageData: pd,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "docs.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
