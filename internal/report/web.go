package report

import (
	"fmt"
	"html"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	api "veloria/internal/api"
	"veloria/internal/auth"
	searchmodel "veloria/internal/search/model"
	uipage "veloria/internal/ui/page"
	"veloria/internal/web"
)

// SubmitReport handles POST /search/{uuid}/report.
// Any authenticated user can report a public search.
func SubmitReport(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		currentUser := auth.UserFromContext(r.Context())
		if currentUser == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		searchIDStr := chi.URLParam(r, "uuid")
		searchID, err := api.ParseID(searchIDStr)
		if err != nil {
			http.Error(w, "invalid search id", http.StatusBadRequest)
			return
		}

		var s searchmodel.Search
		if err := d.DB().First(&s, "id = ?", searchID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				http.Error(w, "search not found", http.StatusNotFound)
				return
			}
			http.Error(w, "failed to load search", http.StatusInternalServerError)
			return
		}

		reason := ""
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
			if err := r.ParseForm(); err == nil {
				reason = strings.TrimSpace(r.FormValue("reason"))
			}
		}

		report := SearchReport{
			SearchID: searchID,
			UserID:   currentUser.ID,
			Reason:   reason,
		}

		result := d.DB().Create(&report)
		if result.Error != nil {
			if strings.Contains(result.Error.Error(), "duplicate key") ||
				strings.Contains(result.Error.Error(), "UNIQUE constraint") {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = fmt.Fprint(w, `<span class="inline-flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-amber-600">Already reported</span>`)
				return
			}
			http.Error(w, "failed to create report", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<span class="inline-flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-amber-600">Reported</span>`)
	}
}

// reportRow is the raw DB result from the reports query.
type reportRow struct {
	ReportID      uuid.UUID
	SearchID      uuid.UUID
	SearchTerm    string
	SearchRepo    string
	SearchPrivate bool
	ReporterName  string
	Reason        string
	ReportedAt    time.Time
}

// ReportsPage renders GET /admin/reports.
func ReportsPage(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.DB() == nil {
			http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
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
		d.DB().Model(&SearchReport{}).Where("resolved = false").Count(&totalCount)
		totalPages := int(math.Ceil(float64(totalCount) / float64(pageSize)))
		if totalPages == 0 {
			totalPages = 1
		}
		if page > totalPages {
			page = totalPages
		}

		offset := (page - 1) * pageSize
		var rows []reportRow
		d.DB().Raw(`
			SELECT sr.id as report_id, sr.search_id, s.term as search_term, s.repo as search_repo,
			       s.private as search_private, u.name as reporter_name, sr.reason, sr.created_at as reported_at
			FROM search_reports sr
			JOIN searches s ON s.id = sr.search_id
			JOIN users u ON u.id = sr.user_id
			WHERE sr.resolved = false
			ORDER BY sr.created_at DESC
			LIMIT ? OFFSET ?
		`, pageSize, offset).Scan(&rows)

		reports := make([]web.ReportedSearchItem, len(rows))
		for i, row := range rows {
			reports[i] = web.ReportedSearchItem{
				ReportID:      row.ReportID.String(),
				SearchID:      row.SearchID.String(),
				SearchTerm:    row.SearchTerm,
				SearchRepo:    row.SearchRepo,
				SearchPrivate: row.SearchPrivate,
				ReporterName:  row.ReporterName,
				Reason:        row.Reason,
				ReportedAt:    row.ReportedAt.Format("2006-01-02 15:04"),
			}
		}

		data := web.ReportsPageData{
			PageData:   d.PageData(r),
			Reports:    reports,
			Page:       page,
			TotalPages: totalPages,
		}

		d.RenderComponent(w, r, uipage.Reports(data))
	}
}

// ResolveReport handles POST /admin/reports/{id}/resolve.
func ResolveReport(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		currentUser := auth.UserFromContext(r.Context())
		if currentUser == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		reportIDStr := chi.URLParam(r, "id")
		reportID, err := uuid.Parse(reportIDStr)
		if err != nil {
			http.Error(w, "invalid report id", http.StatusBadRequest)
			return
		}

		now := time.Now()
		result := d.DB().Model(&SearchReport{}).Where("id = ?", reportID).Updates(map[string]any{
			"resolved":    true,
			"resolved_by": currentUser.ID,
			"resolved_at": now,
		})
		if result.Error != nil {
			http.Error(w, "failed to resolve report", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<span class="inline-flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium rounded-full bg-green-50 text-green-600 border border-green-200">Resolved by %s</span>`, html.EscapeString(currentUser.Name)) // #nosec G705 -- value is HTML-escaped
	}
}
