package search

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	api "veloria/internal/api"
	"veloria/internal/ogimage"
	searchmodel "veloria/internal/search/model"
	"veloria/internal/web"
)

// OGImage serves a dynamically generated Open Graph image for a completed search.
func OGImage(d *web.Deps, gen *ogimage.Generator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "uuid")
		id, err := api.ParseID(idStr)
		if err != nil {
			http.Error(w, "invalid search id", http.StatusBadRequest)
			return
		}

		// Check cache before hitting the database.
		cacheKey := "og:" + id.String()
		if d.Cache != nil {
			if v, ok := d.Cache.Get(cacheKey); ok {
				writeImage(w, v.([]byte))
				return
			}
		}

		if d.DB == nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}

		var s searchmodel.Search
		if err := d.DB.First(&s, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				http.Error(w, "search not found", http.StatusNotFound)
				return
			}
			http.Error(w, "failed to load search", http.StatusInternalServerError)
			return
		}

		if s.Status != searchmodel.StatusCompleted {
			http.Error(w, "search not completed", http.StatusNotFound)
			return
		}

		totalMatches := 0
		if s.TotalMatches != nil {
			totalMatches = *s.TotalMatches
		}
		totalExtensions := 0
		if s.TotalExtensions != nil {
			totalExtensions = *s.TotalExtensions
		}

		imgBytes, err := gen.RenderSearch(s.Term, totalMatches, totalExtensions, s.Repo)
		if err != nil {
			http.Error(w, "failed to generate image", http.StatusInternalServerError)
			return
		}

		// Cache the result — completed searches are immutable.
		if d.Cache != nil {
			d.Cache.Set(cacheKey, imgBytes, 24*time.Hour)
		}

		writeImage(w, imgBytes)
	}
}

func writeImage(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	_, _ = w.Write(data) // #nosec G705 -- content is a generated PNG served with image/png and nosniff
}
