package web

import (
	"net/http"

	"veloria/internal/ui/page"
)

// AboutPage renders the about page.
func AboutPage(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pd := d.PageData(r)
		pd.OG.Title = "About - Veloria"
		pd.OG.Description = "Learn about Veloria, the open-source WordPress code search engine."

		d.RenderComponent(w, r, page.About(pd))
	}
}
