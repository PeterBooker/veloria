package web

import (
	"net/http"

	"veloria/internal/ui/page"
)

// PrivacyPage renders the privacy policy page.
func PrivacyPage(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pd := d.PageData(r)
		pd.OG.Title = "Privacy Policy - Veloria"
		pd.OG.Description = "Veloria's privacy policy — what data we collect and how we use it."

		d.RenderComponent(w, r, page.Privacy(pd))
	}
}

// TermsPage renders the terms of service page.
func TermsPage(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pd := d.PageData(r)
		pd.OG.Title = "Terms of Service - Veloria"
		pd.OG.Description = "Veloria's terms of service — rules and conditions for using the service."

		d.RenderComponent(w, r, page.Terms(pd))
	}
}
