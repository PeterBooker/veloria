package web

import "net/http"

// PrivacyPage renders the privacy policy page.
func PrivacyPage(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pd := d.PageData(r)
		pd.OG.Title = "Privacy Policy - Veloria"
		pd.OG.Description = "Veloria's privacy policy — what data we collect and how we use it."

		data := struct{ PageData }{
			PageData: pd,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "privacy.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// TermsPage renders the terms of service page.
func TermsPage(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pd := d.PageData(r)
		pd.OG.Title = "Terms of Service - Veloria"
		pd.OG.Description = "Veloria's terms of service — rules and conditions for using the service."

		data := struct{ PageData }{
			PageData: pd,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "terms.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
