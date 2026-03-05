package admin

import (
	"errors"
	"fmt"
	"net/http"

	"veloria/internal/manager"
	"veloria/internal/service"
	"veloria/internal/web"
)

// ReindexExtension handles POST /admin/reindex.
// Queues an ad-hoc re-index task for the given extension.
func ReindexExtension(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Reindex() == nil {
			http.Error(w, "Indexer unavailable", http.StatusServiceUnavailable)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		repoType := r.FormValue("repo_type")
		slug := r.FormValue("slug")

		if repoType == "" || slug == "" {
			http.Error(w, "repo_type and slug are required", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		err := d.Reindex().SubmitReindex(repoType, slug)
		if err != nil {
			msg := "Not found"
			if errors.Is(err, manager.ErrQueueFull) {
				msg = "Queue full — try again later"
			}
			_, _ = fmt.Fprintf(w, `<span class="inline-flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-red-600 bg-red-50 border border-red-200 rounded-lg">%s</span>`, msg)
			return
		}

		_, _ = fmt.Fprint(w, `<span class="inline-flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-green-600 bg-green-50 border border-green-200 rounded-lg">Queued for re-index</span>`)
	}
}

// ToggleMaintenance handles POST /admin/maintenance.
// Toggles maintenance mode on or off.
func ToggleMaintenance(reg *service.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		enabled := !reg.InMaintenance()
		reg.SetMaintenance(enabled)

		state := "off"
		if enabled {
			state = "on"
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if enabled {
			_, _ = fmt.Fprintf(w, `<span class="inline-flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-amber-600 bg-amber-50 border border-amber-200 rounded-lg">Maintenance mode %s</span>`, state)
		} else {
			_, _ = fmt.Fprintf(w, `<span class="inline-flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-green-600 bg-green-50 border border-green-200 rounded-lg">Maintenance mode %s</span>`, state)
		}
	}
}
