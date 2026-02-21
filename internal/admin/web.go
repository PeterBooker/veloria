package admin

import (
	"fmt"
	"net/http"

	"veloria/internal/web"
)

// ReindexExtension handles POST /admin/reindex.
// Queues an ad-hoc re-index task for the given extension.
func ReindexExtension(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Manager == nil {
			http.Error(w, "Indexer unavailable", http.StatusServiceUnavailable)
			return
		}

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

		ok := d.Manager.SubmitReindex(repoType, slug)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if !ok {
			fmt.Fprint(w, `<span class="inline-flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-red-600 bg-red-50 border border-red-200 rounded-lg">Not found or queue full</span>`)
			return
		}

		fmt.Fprint(w, `<span class="inline-flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-green-600 bg-green-50 border border-green-200 rounded-lg">Queued for re-index</span>`)
	}
}
