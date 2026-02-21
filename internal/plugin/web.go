package plugin

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"veloria/internal/web"
)

// ViewPage renders a single plugin detail page.
func ViewPage(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.DB == nil {
			http.Error(w, "Plugin data is unavailable while the database is offline.", http.StatusServiceUnavailable)
			return
		}
		slug := chi.URLParam(r, "slug")

		type pluginRow struct {
			ID               uuid.UUID
			Name             string
			Slug             string
			Source           string
			Version          string
			ShortDescription string
			Requires         string
			Tested           string
			RequiresPHP      string
			Rating           int
			ActiveInstalls   int
			Downloaded       int
			DownloadLink     string
			Tags             []byte
			FileCount        int
			TotalSize        int64
			LargestFiles     []byte
		}

		var row pluginRow
		err := d.DB.Table("plugins").
			Select("id, name, slug, source, version, short_description, requires, tested, requires_php, rating, active_installs, downloaded, download_link, tags, file_count, total_size, largest_files").
			Where("slug = ? AND deleted_at IS NULL", slug).
			Scan(&row).Error

		if err != nil || row.ID == uuid.Nil {
			http.Error(w, "Plugin not found", http.StatusNotFound)
			return
		}

		indexStatus := map[string]bool{}
		if d.Manager != nil {
			indexStatus = d.Manager.GetPluginRepo().IndexStatus()
		}

		data := web.ExtensionData{
			PageData:         d.PageData(r),
			RepoType:         "plugins",
			Name:             row.Name,
			Slug:             row.Slug,
			Source:           row.Source,
			Version:          row.Version,
			ShortDescription: row.ShortDescription,
			Author:           "",
			Requires:         row.Requires,
			Tested:           row.Tested,
			RequiresPHP:      row.RequiresPHP,
			Rating:           row.Rating,
			ActiveInstalls:   row.ActiveInstalls,
			Downloaded:       row.Downloaded,
			DownloadLink:     row.DownloadLink,
			Tags:             web.ParseTags(row.Tags),
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
