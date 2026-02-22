package web

import "net/http"

// AboutPage renders the about page.
func AboutPage(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pd := d.PageData(r)
		pd.OG.Title = "About - Veloria"
		pd.OG.Description = "Learn about Veloria, the open-source WordPress code search engine."

		data := struct{ PageData }{
			PageData: pd,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "about.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
