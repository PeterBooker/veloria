package web

import (
	"context"
	"net/http"
	"sync"
	"time"

	searchmodel "veloria/internal/search/model"
	"veloria/internal/auth"
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
		if len(recentSearches) > 0 {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()

			var wg sync.WaitGroup
			for i, s := range recentSearches {
				wg.Add(1)
				go func(idx int, srch searchmodel.Search) {
					defer wg.Done()
					summaries[idx] = BuildSearchSummary(ctx, d.S3, srch)
				}(i, s)
			}
			wg.Wait()
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
			},
			Error: r.URL.Query().Get("error"),
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "login.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
