package search

import (
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"gorm.io/gorm"

	api "veloria/internal/api"
	"veloria/internal/auth"
	"veloria/internal/index"
	"veloria/internal/manager"
	searchmodel "veloria/internal/search/model"
	"veloria/internal/telemetry"
	typespb "veloria/internal/types"
	uipage "veloria/internal/ui/page"
	"veloria/internal/ui/partial"
	"veloria/internal/web"
)

// ViewPage renders a single search page with grouped results.
func ViewPage(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.DB() == nil {
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
		if err := d.DB().First(&s, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				http.Error(w, "search not found", http.StatusNotFound)
				return
			}
			http.Error(w, "failed to load search", http.StatusInternalServerError)
			return
		}

		pd := d.PageData(r)
		pd.OG.Title = fmt.Sprintf("Search \"%s\" - Veloria", s.Term)
		pd.OG.Description = fmt.Sprintf("Code search for \"%s\" in %s.", s.Term, s.Repo)

		data := web.SearchViewData{
			PageData: pd,
			Search:   s,
		}
		if s.Private && shouldForcePrivate(s.Term) {
			data.ModerationNotice = "This search has been automatically set to private because the search term contains a URL or inappropriate language."
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
				data.MatchCapReached = *s.TotalMatches >= 100_000
			}
			if s.TotalExtensions != nil {
				data.TotalExtensions = *s.TotalExtensions
			}
			data.OG.Description = fmt.Sprintf(
				"%d matches across %d %s for \"%s\".",
				data.TotalMatches, data.TotalExtensions, s.Repo, s.Term,
			)
			appURL := d.Config.AppURL
			if appURL != "" {
				data.OG.Image = appURL + "/search/" + s.ID.String() + "/og.png"
			}
		}

		w.Header().Set("Cache-Control", "no-store")
		d.RenderComponent(w, r, uipage.SearchView(data))
	}
}

// SearchExtensionsPartial renders the paginated, searchable extension list as an HTMX partial.
func SearchExtensionsPartial(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.DB() == nil || d.S3() == nil {
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
		if err := d.DB().First(&s, "id = ?", id).Error; err != nil {
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
		if err := d.S3().DownloadResult(ctx, s.ID.String(), &protoResults); err != nil {
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
			SearchID:         s.ID.String(),
			SearchDataSource: s.Repo,
			Extensions:       summaries[start:end],
			Page:             page,
			TotalPages:       totalPages,
			Search:           r.URL.Query().Get("search"),
		}

		d.RenderComponent(w, r, partial.SearchExtensions(data))
	}
}

// ExportCSV streams the search results as a CSV download.
func ExportCSV(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.DB() == nil || d.S3() == nil {
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
		if err := d.DB().First(&s, "id = ?", id).Error; err != nil {
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
		if err := d.S3().DownloadResult(ctx, s.ID.String(), &protoResults); err != nil {
			http.Error(w, "failed to load results", http.StatusInternalServerError)
			return
		}

		results := searchmodel.SearchResponseFromProto(&protoResults)

		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="search-%s.csv"`, s.ID))

		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"Extension", "Slug", "Version", "Active Installs", "File", "Line Number", "Line"})

		for _, result := range results.Results {
			for _, fm := range result.Matches {
				for _, m := range fm.Matches {
					_ = cw.Write([]string{
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
			if r := d.Registry.SearchDisabledReason(); r != "" {
				reason = r
			}
			renderSearchError(d, w, r, reason)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
		if err := r.ParseForm(); err != nil {
			renderSearchError(d, w, r, "Failed to parse form")
			return
		}

		term := r.FormValue("term")
		repo := r.FormValue("repo")
		visibility := r.FormValue("visibility")
		filetype := r.FormValue("filetype")
		caseSensitive := r.FormValue("case_sensitive") == "on"
		excludeMinified := r.FormValue("exclude_minified") == "on"

		if term == "" {
			renderSearchError(d, w, r, "Search term is required")
			return
		}
		if err := index.ValidatePattern(term); err != nil {
			renderSearchError(d, w, r, "Invalid regex pattern: "+err.Error())
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
		if !private && shouldForcePrivate(term) {
			private = true
		}

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
		if err := d.DB().Create(&s).Error; err != nil {
			renderSearchError(d, w, r, "Failed to create search")
			return
		}

		go runSearchAsync(d, s.ID, repo, term, fileMatch, excludeMatch, !caseSensitive) // #nosec G118 -- goroutine intentionally outlives request; search runs in background

		w.Header().Set("HX-Redirect", "/search/"+s.ID.String())
		w.WriteHeader(http.StatusOK)
	}
}

func runSearchAsync(d *web.Deps, searchID uuid.UUID, repo, term, fileMatch, excludeMatch string, caseInsensitive bool) {
	d.DB().Model(&searchmodel.Search{}).Where("id = ?", searchID).Update("status", searchmodel.StatusProcessing)
	defer d.Progress.Delete(searchID)

	searchCtx, searchCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer searchCancel()

	searchStart := time.Now()
	results, err := d.Search().Search(searchCtx, repo, term, &manager.SearchParams{
		FileMatch:        fileMatch,
		ExcludeFileMatch: excludeMatch,
		CaseInsensitive:  caseInsensitive,
		OnProgress: func(searched, total int) {
			d.Progress.Set(searchID, searched, total)
		},
	})
	searchElapsed := time.Since(searchStart).Seconds()

	attrs := metric.WithAttributes(
		attribute.String("source", repo),
		attribute.String("type", "web"),
	)
	telemetry.SearchCount.Add(context.Background(), 1, attrs)
	telemetry.SearchDuration.Record(context.Background(), searchElapsed, attrs)

	if err != nil {
		slog.Error("search failed", "id", searchID, "term", term, "error", err)
		d.DB().Model(&searchmodel.Search{}).Where("id = ?", searchID).Update("status", searchmodel.StatusFailed)
		return
	}

	now := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	protoResults := searchmodel.SearchResponseToProto(results)
	size, err := d.S3().UploadResult(ctx, searchID.String(), protoResults)
	if err != nil {
		slog.Error("search result upload failed", "id", searchID, "term", term, "error", err)
		d.DB().Model(&searchmodel.Search{}).Where("id = ?", searchID).Update("status", searchmodel.StatusFailed)
		return
	}

	totalMatches := web.CountTotalMatches(results)

	d.DB().Model(&searchmodel.Search{}).Where("id = ?", searchID).Updates(map[string]any{
		"status":           searchmodel.StatusCompleted,
		"results_size":     size,
		"completed_at":     now,
		"total_matches":    totalMatches,
		"total_extensions": len(results.Results),
	})
}

func renderSearchError(d *web.Deps, w http.ResponseWriter, r *http.Request, errMsg string) {
	d.RenderComponent(w, r, partial.SearchError(errMsg))
}

// ListPage renders the public searches list page.
func ListPage(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		listSearches(d, w, r, "")
	}
}

// ListOwnPage renders the current user's searches list page.
func ListOwnPage(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		listSearches(d, w, r, "own")
	}
}

// MyListRedirect permanently redirects /my-searches to /searches/own.
func MyListRedirect() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := "/searches/own"
		if pageStr := r.URL.Query().Get("page"); pageStr != "" {
			target += "?page=" + pageStr
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	}
}

func listSearches(d *web.Deps, w http.ResponseWriter, r *http.Request, view string) {
	if d.DB() == nil {
		http.Error(w, "Searches are unavailable while the database is offline.", http.StatusServiceUnavailable)
		return
	}

	currentUser := auth.UserFromContext(r.Context())

	if view == "own" && currentUser == nil {
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

	query := d.DB().Model(&searchmodel.Search{})
	if view == "own" {
		query = query.Where("user_id = ?", currentUser.ID)
	} else {
		query = query.Where("private = false")
	}

	var totalCount int64
	query.Count(&totalCount)
	totalPages := int(math.Ceil(float64(totalCount) / float64(pageSize)))
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	offset := (page - 1) * pageSize
	var searches []searchmodel.Search
	query.Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&searches)

	summaries := make([]web.SearchSummary, len(searches))
	for i, s := range searches {
		summaries[i] = web.BuildSearchSummary(s)
	}

	pd := d.PageData(r)
	if view == "own" {
		pd.OG.Title = "My Searches - Veloria"
		pd.OG.Description = "View and manage your WordPress code searches on Veloria."
	} else {
		pd.OG.Title = "Recent Searches - Veloria"
		pd.OG.Description = "Browse recent WordPress code searches on Veloria."
	}

	data := web.SearchesData{
		PageData:   pd,
		Searches:   summaries,
		Page:       page,
		TotalPages: totalPages,
		View:       view,
		LoggedIn:   currentUser != nil,
	}

	d.RenderComponent(w, r, uipage.Searches(data))
}

// ExtensionResultsPage renders the detailed match results for a single extension within a search.
func ExtensionResultsPage(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.DB() == nil || d.S3() == nil {
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
		if err := d.DB().First(&s, "id = ?", id).Error; err != nil {
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
		if err := d.S3().DownloadResult(ctx, s.ID.String(), &protoResults); err != nil {
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
			SearchID:         s.ID.String(),
			SearchDataSource: s.Repo,
			Result:           match,
		}

		d.RenderComponent(w, r, partial.SearchExtensionResults(data))
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
			DataSource: repoType,
			Slug:       slug,
			Filename:   filename,
		}
		if d.Sources() == nil {
			data.Error = "Search context is unavailable."
			renderSearchContext(d, w, r, data)
			return
		}

		lineNumber, err := strconv.Atoi(lineStr)
		if err != nil || lineNumber <= 0 {
			data.Error = "Invalid line number."
			renderSearchContext(d, w, r, data)
			return
		}

		if repoType == "" || slug == "" || filename == "" {
			data.Error = "Missing context details."
			renderSearchContext(d, w, r, data)
			return
		}

		sourceDir, err := resolveSourceDir(d, repoType, slug)
		if err != nil {
			data.Error = err.Error()
			renderSearchContext(d, w, r, data)
			return
		}

		fullPath, err := web.SafeJoin(sourceDir, filename)
		if err != nil {
			data.Error = "Invalid file path."
			renderSearchContext(d, w, r, data)
			return
		}

		lines, err := web.ReadContextLines(fullPath, lineNumber, 4)
		if err != nil {
			data.Error = "Unable to load file context."
			renderSearchContext(d, w, r, data)
			return
		}

		data.Lines = lines
		renderSearchContext(d, w, r, data)
	}
}

func renderSearchContext(d *web.Deps, w http.ResponseWriter, r *http.Request, data web.SearchContextData) {
	d.RenderComponent(w, r, partial.SearchContext(data))
}

func resolveSourceDir(d *web.Deps, repoType string, slug string) (string, error) {
	return d.Sources().ResolveSourceDir(repoType, slug)
}
