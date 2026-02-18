package web

import (
	"net/http"

	"veloria/internal/auth"
	"veloria/internal/config"
	searchmodel "veloria/internal/search/model"
)

// HomePage renders the home page with search form.
func HomePage(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var recentSearches []searchmodel.Search
		if d.DB != nil {
			d.DB.Where("status = ? AND private = false", searchmodel.StatusCompleted).
				Order("created_at DESC").
				Limit(5).
				Find(&recentSearches)
		}

		summaries := make([]SearchSummary, len(recentSearches))
		for i, s := range recentSearches {
			summaries[i] = BuildSearchSummary(s)
		}

		data := HomeData{
			PageData:       d.PageData(r),
			RecentSearches: summaries,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "home.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// LoginPage renders the login page.
func LoginPage(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := auth.UserFromContext(r.Context())
		if u != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		data := LoginData{
			PageData: PageData{
				EnabledProviders:     auth.GetEnabledProviders(d.Config),
				SearchEnabled:        d.SearchEnabled,
				SearchDisabledReason: d.SearchDisabledReason,
				CurrentPath:          r.URL.Path,
				Version:              config.Version,
			},
			Error: r.URL.Query().Get("error"),
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "login.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
