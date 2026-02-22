package search

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	api "veloria/internal/api"
	searchmodel "veloria/internal/search/model"
	"veloria/internal/web"
)

// ToggleVisibility handles POST /admin/search/{uuid}/visibility.
// Toggles a search between public and private.
func ToggleVisibility(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.DB == nil {
			http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
			return
		}

		searchIDStr := chi.URLParam(r, "uuid")
		searchID, err := api.ParseID(searchIDStr)
		if err != nil {
			http.Error(w, "invalid search id", http.StatusBadRequest)
			return
		}

		var s searchmodel.Search
		if err := d.DB.First(&s, "id = ?", searchID).Error; err != nil {
			http.Error(w, "search not found", http.StatusNotFound)
			return
		}

		newPrivate := !s.Private
		d.DB.Model(&s).Update("private", newPrivate)

		data := web.VisibilityToggleData{
			SearchID: searchID.String(),
			Private:  newPrivate,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "admin_visibility_toggle.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
