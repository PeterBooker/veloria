package plugin

import (
	"html"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"veloria/internal/ui/page"
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
		if d.Stats != nil {
			indexStatus = d.Stats.IndexStatus("plugins")
		}

		pd := d.PageData(r)
		pd.OG.Title = html.UnescapeString(row.Name) + " - Veloria"
		if row.ShortDescription != "" {
			pd.OG.Description = row.ShortDescription
		}

		data := web.ExtensionData{
			PageData:         pd,
			RepoType:         "plugins",
			Name:             html.UnescapeString(row.Name),
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

		d.RenderComponent(w, r, page.Extension(data))
	}
}
