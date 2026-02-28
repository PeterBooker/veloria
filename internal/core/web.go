package core

import (
	"html"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"veloria/internal/ui/page"
	"veloria/internal/web"
)

// ViewPage renders a single core version detail page.
func ViewPage(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.DB == nil {
			http.Error(w, "Core data is unavailable while the database is offline.", http.StatusServiceUnavailable)
			return
		}
		version := chi.URLParam(r, "version")

		type coreRow struct {
			ID           uuid.UUID
			Name         string
			Version      string
			DownloadLink string
			FileCount    int
			TotalSize    int64
			LargestFiles []byte
		}

		var row coreRow
		err := d.DB.Table("cores").
			Select("id, name, version, zip_url AS download_link, file_count, total_size, largest_files").
			Where("version = ? AND deleted_at IS NULL", version).
			Scan(&row).Error

		if err != nil || row.ID == uuid.Nil {
			http.Error(w, "Core version not found", http.StatusNotFound)
			return
		}

		indexStatus := map[string]bool{}
		if d.Stats != nil {
			indexStatus = d.Stats.IndexStatus("cores")
		}

		pd := d.PageData(r)
		pd.OG.Title = html.UnescapeString(row.Name) + " - Veloria"
		pd.OG.Description = "WordPress " + row.Version + " core source code indexed by Veloria."

		data := web.ExtensionData{
			PageData:     pd,
			RepoType:     "cores",
			Name:         html.UnescapeString(row.Name),
			Slug:         row.Version,
			Version:      row.Version,
			DownloadLink: row.DownloadLink,
			Indexed:      indexStatus[row.Version],
			FileCount:    row.FileCount,
			TotalSize:    row.TotalSize,
			LargestFiles: web.ParseLargestFiles(row.LargestFiles, 25),
		}

		d.RenderComponent(w, r, page.Extension(data))
	}
}
