package search

import (
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	api "veloria/internal/api"
	"veloria/internal/auth"
	"veloria/internal/manager"
	searchmodel "veloria/internal/search/model"
	typespb "veloria/internal/types"
	"veloria/internal/web"
)

// ViewPage renders a single search page with grouped results.
func ViewPage(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.DB == nil {
			http.Error(w, "Searches are unavailable while the database is offline.", http.StatusServiceUnavailable)
			return
		}
		idStr := chi.URLParam(r, "uuid")
		id, err := api.ParseID(idStr)
		if err != nil {
			http.Error(w, "invalid search id", http.StatusBadRequest)
			return
		}
		if idStr != id.String() {
			http.Redirect(w, r, "/search/"+id.String(), http.StatusMovedPermanently)
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

		data := web.SearchViewData{
			PageData: d.PageData(r),
			Search:   s,
		}
		if s.CompletedAt != nil {
			data.DurationMs = s.CompletedAt.Sub(s.CreatedAt).Milliseconds()
		}

		if s.Status == searchmodel.StatusProcessing {
			if p, ok := d.Progress.Get(s.ID); ok {
				data.ProgressSearched = p.Searched
				data.ProgressTotal = p.Total
			}
		}

		if s.Status == searchmodel.StatusCompleted {
			if s.TotalMatches != nil {
				data.TotalMatches = *s.TotalMatches
			}
			if s.TotalExtensions != nil {
				data.TotalExtensions = *s.TotalExtensions
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := d.Templates.Render(w, "search.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// SearchExtensionsPartial renders the paginated, searchable extension list as an HTMX partial.
func SearchExtensionsPartial(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.DB == nil || d.S3 == nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}

		idStr := chi.URLParam(r, "uuid")
		id, err := api.ParseID(idStr)
		if err != nil {
			http.Error(w, "invalid search id", http.StatusBadRequest)
			return
		}
		if idStr != id.String() {
			q := r.URL.Query().Encode()
			target := "/search/" + id.String() + "/extensions"
			if q != "" {
				target += "?" + q
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}

		var s searchmodel.Search
		if err := d.DB.First(&s, "id = ?", id).Error; err != nil {
			http.Error(w, "search not found", http.StatusNotFound)
			return
		}
		if s.Status != searchmodel.StatusCompleted {
			http.Error(w, "search not completed", http.StatusNotFound)
			return
		}

		const pageSize = 25
		page := 1
		if pageStr := r.URL.Query().Get("page"); pageStr != "" {
			if parsed, err := strconv.Atoi(pageStr); err == nil && parsed > 0 {
				page = parsed
			}
		}
		search := strings.ToLower(r.URL.Query().Get("search"))

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		var protoResults typespb.SearchResponse
		if err := d.S3.DownloadResult(ctx, s.ID.String(), &protoResults); err != nil {
			http.Error(w, "failed to load results", http.StatusInternalServerError)
			return
		}

		results := searchmodel.SearchResponseFromProto(&protoResults)

		// Build summaries, filtering by search term if provided.
		var summaries []web.ExtensionResultSummary
		for _, result := range results.Results {
			if search != "" {
				if !strings.Contains(strings.ToLower(result.Name), search) &&
					!strings.Contains(strings.ToLower(result.Slug), search) {
					continue
				}
			}
			summaries = append(summaries, web.ExtensionResultSummary{
				Slug:           result.Slug,
				Name:           result.Name,
				Version:        result.Version,
				ActiveInstalls: result.ActiveInstalls,
				TotalMatches:   result.TotalMatches,
			})
		}

		total := len(summaries)
		totalPages := int(math.Ceil(float64(total) / float64(pageSize)))
		if totalPages == 0 {
			totalPages = 1
		}
		if page > totalPages {
			page = totalPages
		}

		// Paginate the slice.
		start := (page - 1) * pageSize
		end := start + pageSize
		if start > len(summaries) {
			start = len(summaries)
		}
		if end > len(summaries) {
			end = len(summaries)
		}

		data := web.SearchExtensionsData{
			SearchID:   s.ID.String(),
			SearchRepo: s.Repo,
			Extensions: summaries[start:end],
			Page:       page,
			TotalPages: totalPages,
			Search:     r.URL.Query().Get("search"),
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "search-extensions.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// ExportCSV streams the search results as a CSV download.
func ExportCSV(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.DB == nil || d.S3 == nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}

		idStr := chi.URLParam(r, "uuid")
		id, err := api.ParseID(idStr)
		if err != nil {
			http.Error(w, "invalid search id", http.StatusBadRequest)
			return
		}

		var s searchmodel.Search
		if err := d.DB.First(&s, "id = ?", id).Error; err != nil {
			http.Error(w, "search not found", http.StatusNotFound)
			return
		}
		if s.Status != searchmodel.StatusCompleted {
			http.Error(w, "search not completed", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		var protoResults typespb.SearchResponse
		if err := d.S3.DownloadResult(ctx, s.ID.String(), &protoResults); err != nil {
			http.Error(w, "failed to load results", http.StatusInternalServerError)
			return
		}

		results := searchmodel.SearchResponseFromProto(&protoResults)

		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="search-%s.csv"`, s.ID))

		cw := csv.NewWriter(w)
		cw.Write([]string{"Extension", "Slug", "Version", "Active Installs", "File", "Line Number", "Line"})

		for _, result := range results.Results {
			for _, fm := range result.Matches {
				for _, m := range fm.Matches {
					cw.Write([]string{
						result.Name,
						result.Slug,
						result.Version,
						strconv.Itoa(result.ActiveInstalls),
						fm.Filename,
						strconv.Itoa(m.LineNumber),
						m.Line,
					})
				}
			}
		}

		cw.Flush()
	}
}

// SubmitSearch handles search form submissions by creating a search record
// and redirecting to the search view page, which polls for results.
func SubmitSearch(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !d.SearchAvailable() {
			w.WriteHeader(http.StatusServiceUnavailable)
			reason := "Search is temporarily unavailable."
			if d.SearchDisabledReason != "" {
				reason = d.SearchDisabledReason
			}
			renderSearchError(d, w, reason)
			return
		}
		if err := r.ParseForm(); err != nil {
			renderSearchError(d, w, "Failed to parse form")
			return
		}

		term := r.FormValue("term")
		repo := r.FormValue("repo")
		visibility := r.FormValue("visibility")
		filetype := r.FormValue("filetype")
		caseSensitive := r.FormValue("case_sensitive") == "on"
		excludeMinified := r.FormValue("exclude_minified") == "on"

		if term == "" {
			renderSearchError(d, w, "Search term is required")
			return
		}
		if repo == "" {
			repo = "plugins"
		}

		var fileMatch string
		if filetype != "" {
			fileMatch = `\.` + regexp.QuoteMeta(filetype) + `$`
		}

		var excludeMatch string
		if excludeMinified {
			excludeMatch = `\.min\.(js|css)$`
		}

		private := visibility == "private"

		currentUser := auth.UserFromContext(r.Context())

		s := searchmodel.Search{
			Status:  searchmodel.StatusQueued,
			Private: private,
			Term:    term,
			Repo:    repo,
		}
		if currentUser != nil {
			s.UserID = &currentUser.ID
		}
		if err := d.DB.Create(&s).Error; err != nil {
			renderSearchError(d, w, "Failed to create search")
			return
		}

		go runSearchAsync(d, s.ID, repo, term, fileMatch, excludeMatch, !caseSensitive)

		w.Header().Set("HX-Redirect", "/search/"+s.ID.String())
		w.WriteHeader(http.StatusOK)
	}
}

func runSearchAsync(d *web.Deps, searchID uuid.UUID, repo, term, fileMatch, excludeMatch string, caseInsensitive bool) {
	d.DB.Model(&searchmodel.Search{}).Where("id = ?", searchID).Update("status", searchmodel.StatusProcessing)
	defer d.Progress.Delete(searchID)

	results, err := d.Search.Search(repo, term, &manager.SearchParams{
		FileMatch:        fileMatch,
		ExcludeFileMatch: excludeMatch,
		CaseInsensitive:  caseInsensitive,
		OnProgress: func(searched, total int) {
			d.Progress.Set(searchID, searched, total)
		},
	})
	if err != nil {
		d.DB.Model(&searchmodel.Search{}).Where("id = ?", searchID).Update("status", searchmodel.StatusFailed)
		return
	}

	now := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	protoResults := searchmodel.SearchResponseToProto(results)
	size, err := d.S3.UploadResult(ctx, searchID.String(), protoResults)
	if err != nil {
		d.DB.Model(&searchmodel.Search{}).Where("id = ?", searchID).Update("status", searchmodel.StatusFailed)
		return
	}

	totalMatches := web.CountTotalMatches(results)

	d.DB.Model(&searchmodel.Search{}).Where("id = ?", searchID).Updates(map[string]any{
		"status":           searchmodel.StatusCompleted,
		"results_size":     size,
		"completed_at":     now,
		"total_matches":    totalMatches,
		"total_extensions": results.Total,
	})
}

func renderSearchError(d *web.Deps, w http.ResponseWriter, errMsg string) {
	data := web.SearchResultsData{Error: errMsg}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := d.Templates.Render(w, "search_results.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ListPage renders the public searches list page.
func ListPage(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.DB == nil {
			http.Error(w, "Searches are unavailable while the database is offline.", http.StatusServiceUnavailable)
			return
		}
		const pageSize = 25
		page := 1
		if pageStr := r.URL.Query().Get("page"); pageStr != "" {
			if parsed, err := strconv.Atoi(pageStr); err == nil && parsed > 0 {
				page = parsed
			}
		}

		var totalCount int64
		d.DB.Model(&searchmodel.Search{}).Where("private = false").Count(&totalCount)
		totalPages := int(math.Ceil(float64(totalCount) / float64(pageSize)))
		if totalPages == 0 {
			totalPages = 1
		}
		if page > totalPages {
			page = totalPages
		}

		offset := (page - 1) * pageSize
		var searches []searchmodel.Search
		d.DB.Where("private = false").Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&searches)

		summaries := make([]web.SearchSummary, len(searches))
		for i, s := range searches {
			summaries[i] = web.BuildSearchSummary(s)
		}

		data := web.SearchesData{
			PageData:   d.PageData(r),
			Searches:   summaries,
			Page:       page,
			TotalPages: totalPages,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "searches.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// MyListPage renders the current user's searches list page.
func MyListPage(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.DB == nil {
			http.Error(w, "Searches are unavailable while the database is offline.", http.StatusServiceUnavailable)
			return
		}
		currentUser := auth.UserFromContext(r.Context())
		if currentUser == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		const pageSize = 25
		page := 1
		if pageStr := r.URL.Query().Get("page"); pageStr != "" {
			if parsed, err := strconv.Atoi(pageStr); err == nil && parsed > 0 {
				page = parsed
			}
		}

		var totalCount int64
		d.DB.Model(&searchmodel.Search{}).Where("user_id = ?", currentUser.ID).Count(&totalCount)
		totalPages := int(math.Ceil(float64(totalCount) / float64(pageSize)))
		if totalPages == 0 {
			totalPages = 1
		}
		if page > totalPages {
			page = totalPages
		}

		offset := (page - 1) * pageSize
		var searches []searchmodel.Search
		d.DB.Where("user_id = ?", currentUser.ID).Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&searches)

		summaries := make([]web.SearchSummary, len(searches))
		for i, s := range searches {
			summaries[i] = web.BuildSearchSummary(s)
		}

		data := web.MySearchesData{
			PageData:   d.PageData(r),
			Searches:   summaries,
			Page:       page,
			TotalPages: totalPages,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "my_searches.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// ExtensionResultsPage renders the detailed match results for a single extension within a search.
func ExtensionResultsPage(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.DB == nil || d.S3 == nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}

		idStr := chi.URLParam(r, "uuid")
		id, err := api.ParseID(idStr)
		if err != nil {
			http.Error(w, "invalid search id", http.StatusBadRequest)
			return
		}
		slug := chi.URLParam(r, "slug")
		if idStr != id.String() {
			http.Redirect(w, r, "/search/"+id.String()+"/extension/"+slug, http.StatusMovedPermanently)
			return
		}
		if slug == "" {
			http.Error(w, "missing slug", http.StatusBadRequest)
			return
		}

		var s searchmodel.Search
		if err := d.DB.First(&s, "id = ?", id).Error; err != nil {
			http.Error(w, "search not found", http.StatusNotFound)
			return
		}
		if s.Status != searchmodel.StatusCompleted {
			http.Error(w, "search not completed", http.StatusNotFound)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		var protoResults typespb.SearchResponse
		if err := d.S3.DownloadResult(ctx, s.ID.String(), &protoResults); err != nil {
			http.Error(w, "failed to load results", http.StatusInternalServerError)
			return
		}

		results := searchmodel.SearchResponseFromProto(&protoResults)
		var match *manager.SearchResult
		for _, result := range results.Results {
			if result.Slug == slug {
				match = result
				break
			}
		}
		if match == nil {
			http.Error(w, "extension not found in results", http.StatusNotFound)
			return
		}

		data := web.ExtensionResultsData{
			SearchID:   s.ID.String(),
			SearchRepo: s.Repo,
			Result:     match,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "search_extension_results.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// ContextPage renders a modal with lines around a specific match.
func ContextPage(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repoType := r.URL.Query().Get("repo")
		slug := r.URL.Query().Get("slug")
		filename := r.URL.Query().Get("file")
		lineStr := r.URL.Query().Get("line")

		data := web.SearchContextData{
			Repo:     repoType,
			Slug:     slug,
			Filename: filename,
		}
		if d.Sources == nil {
			data.Error = "Search context is unavailable."
			renderSearchContext(d, w, data)
			return
		}

		lineNumber, err := strconv.Atoi(lineStr)
		if err != nil || lineNumber <= 0 {
			data.Error = "Invalid line number."
			renderSearchContext(d, w, data)
			return
		}

		if repoType == "" || slug == "" || filename == "" {
			data.Error = "Missing context details."
			renderSearchContext(d, w, data)
			return
		}

		sourceDir, err := resolveSourceDir(d, repoType, slug)
		if err != nil {
			data.Error = err.Error()
			renderSearchContext(d, w, data)
			return
		}

		fullPath, err := web.SafeJoin(sourceDir, filename)
		if err != nil {
			data.Error = "Invalid file path."
			renderSearchContext(d, w, data)
			return
		}

		lines, err := web.ReadContextLines(fullPath, lineNumber, 4)
		if err != nil {
			data.Error = "Unable to load file context."
			renderSearchContext(d, w, data)
			return
		}

		data.Lines = lines
		renderSearchContext(d, w, data)
	}
}

func renderSearchContext(d *web.Deps, w http.ResponseWriter, data web.SearchContextData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := d.Templates.Render(w, "search_context.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func resolveSourceDir(d *web.Deps, repoType string, slug string) (string, error) {
	return d.Sources.ResolveSourceDir(repoType, slug)
}
