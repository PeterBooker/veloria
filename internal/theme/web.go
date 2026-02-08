package theme

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"veloria/internal/web"
)

// ViewPage renders a single theme detail page.
func ViewPage(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.DB == nil {
			http.Error(w, "Theme data is unavailable while the database is offline.", http.StatusServiceUnavailable)
			return
		}
		slug := chi.URLParam(r, "slug")

		type themeRow struct {
			ID               uuid.UUID
			Name             string
			Slug             string
			Version          string
			ShortDescription string
			ActiveInstalls   int
			Downloaded       int
			FileCount        int
			TotalSize        int64
			LargestFiles     []byte
		}

		var row themeRow
		err := d.DB.Table("themes").
			Select("id, name, slug, version, short_description, active_installs, downloaded, file_count, total_size, largest_files").
			Where("slug = ? AND deleted_at IS NULL", slug).
			Scan(&row).Error

		if err != nil || row.ID == uuid.Nil {
			http.Error(w, "Theme not found", http.StatusNotFound)
			return
		}

		indexStatus := map[string]bool{}
		if d.Manager != nil {
			indexStatus = d.Manager.GetThemeRepo().IndexStatus()
		}

		data := web.ExtensionData{
			PageData:         d.PageData(r),
			RepoType:         "themes",
			Name:             row.Name,
			Slug:             row.Slug,
			Version:          row.Version,
			ShortDescription: row.ShortDescription,
			ActiveInstalls:   row.ActiveInstalls,
			Downloaded:       row.Downloaded,
			Indexed:          indexStatus[row.Slug],
			FileCount:        row.FileCount,
			TotalSize:        row.TotalSize,
			LargestFiles:     web.ParseLargestFiles(row.LargestFiles, 25),
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "extension.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
