package core

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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
			FileCount    int
			TotalSize    int64
			LargestFiles []byte
		}

		var row coreRow
		err := d.DB.Table("cores").
			Select("id, name, version, file_count, total_size, largest_files").
			Where("version = ? AND deleted_at IS NULL", version).
			Scan(&row).Error

		if err != nil || row.ID == uuid.Nil {
			http.Error(w, "Core version not found", http.StatusNotFound)
			return
		}

		indexStatus := map[string]bool{}
		if d.Manager != nil {
			indexStatus = d.Manager.GetCoreRepo().IndexStatus()
		}

		data := web.ExtensionData{
			PageData:     d.PageData(r),
			RepoType:     "cores",
			Name:         row.Name,
			Slug:         row.Version,
			Version:      row.Version,
			Indexed:      indexStatus[row.Version],
			FileCount:    row.FileCount,
			TotalSize:    row.TotalSize,
			LargestFiles: web.ParseLargestFiles(row.LargestFiles, 25),
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "extension.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
