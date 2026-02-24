package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"veloria/internal/manager"
	searchmodel "veloria/internal/search/model"
	"veloria/internal/storage"
	typespb "veloria/internal/types"
)

// SearchService abstracts data access for MCP tools.
// DirectService implements it using the Manager directly (for the remote server).
// APIService implements it using Veloria's REST API (for the stdio binary).
type SearchService interface {
	// Search executes a new code search and persists results.
	// Returns the search ID alongside the response so callers can paginate later.
	Search(ctx context.Context, params SearchParams) (searchID string, resp *SearchResponse, err error)

	// LoadSearch retrieves previously persisted search results by ID.
	LoadSearch(ctx context.Context, searchID string) (*SearchResponse, error)

	// ListExtensions returns a paginated list of extensions.
	ListExtensions(ctx context.Context, params ListParams) (*ListResponse, error)
}

// searchSem limits concurrent MCP searches to 1, matching the REST API's
// concurrency model (see internal/search/api.go:searchSem). Without this,
// concurrent MCP tool calls could overload the trigram search engine.
var searchSem = make(chan struct{}, 1)

func acquireSearchSlot(ctx context.Context) error {
	select {
	case searchSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseSearchSlot() {
	<-searchSem
}

// DirectService implements SearchService using the Manager, DB, and S3 directly.
type DirectService struct {
	manager *manager.Manager
	db      *gorm.DB
	s3      storage.ResultStorage
}

// NewDirectService creates a DirectService backed by the application's
// Manager (for search), DB (for search records), and S3 (for result persistence).
func NewDirectService(m *manager.Manager, db *gorm.DB, s3 storage.ResultStorage) *DirectService {
	return &DirectService{manager: m, db: db, s3: s3}
}

func (s *DirectService) Search(ctx context.Context, params SearchParams) (string, *SearchResponse, error) {
	if err := acquireSearchSlot(ctx); err != nil {
		return "", nil, fmt.Errorf("search queue full, try again later")
	}
	defer releaseSearchSlot()

	results, err := s.manager.Search(params.Repo, params.Query, &manager.SearchParams{
		FileMatch:        params.FileMatch,
		ExcludeFileMatch: params.ExcludeFileMatch,
		CaseInsensitive:  !params.CaseSensitive,
		LinesOfContext:   uint(params.ContextLines),
	})
	if err != nil {
		return "", nil, fmt.Errorf("search failed: %w", err)
	}

	resp := convertManagerResponse(results)

	// Persist to DB + S3 so pagination loads from cache instead of re-searching.
	searchID, err := s.persistSearch(ctx, params, results, resp)
	if err != nil {
		// Non-fatal: search still succeeded, pagination just won't work.
		// Return results without a search_id.
		return "", resp, nil
	}

	return searchID, resp, nil
}

func (s *DirectService) LoadSearch(ctx context.Context, searchID string) (*SearchResponse, error) {
	id, err := uuid.Parse(searchID)
	if err != nil {
		return nil, fmt.Errorf("invalid search ID")
	}

	var search searchmodel.Search
	if err := s.db.WithContext(ctx).First(&search, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("search not found")
		}
		return nil, fmt.Errorf("failed to load search")
	}

	if search.Status != searchmodel.StatusCompleted {
		return nil, fmt.Errorf("search not completed (status: %s)", search.Status)
	}

	var protoResults typespb.SearchResponse
	if err := s.s3.DownloadResult(ctx, searchID, &protoResults); err != nil {
		return nil, fmt.Errorf("failed to load search results")
	}

	managerResp := searchmodel.SearchResponseFromProto(&protoResults)
	return convertManagerResponse(managerResp), nil
}

// persistSearch creates a DB record and uploads results to S3.
func (s *DirectService) persistSearch(ctx context.Context, params SearchParams, results *manager.SearchResponse, resp *SearchResponse) (string, error) {
	if s.db == nil || s.s3 == nil {
		return "", fmt.Errorf("persistence unavailable")
	}

	now := time.Now()
	search := searchmodel.Search{
		Status:  searchmodel.StatusCompleted,
		Private: true, // MCP searches are private by default
		Term:    params.Query,
		Repo:    params.Repo,
	}
	search.CompletedAt = &now
	search.TotalMatches = &resp.TotalMatches
	search.TotalExtensions = &resp.TotalExtensions

	if err := s.db.WithContext(ctx).Create(&search).Error; err != nil {
		return "", err
	}

	protoResults := searchmodel.SearchResponseToProto(results)
	uploadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	size, err := s.s3.UploadResult(uploadCtx, search.ID.String(), protoResults)
	if err != nil {
		// Clean up the DB record on upload failure.
		s.db.Delete(&search)
		return "", err
	}

	s.db.Model(&search).Updates(map[string]any{
		"results_size": size,
	})

	return search.ID.String(), nil
}

func (s *DirectService) ListExtensions(ctx context.Context, params ListParams) (*ListResponse, error) {
	table := params.Repo
	if table == "" {
		table = "plugins"
	}

	if !isValidRepo(table) {
		return nil, fmt.Errorf("unknown repo: %s", table)
	}

	slugCol := "slug"
	if table == "cores" {
		slugCol = "version"
	}

	query := s.db.WithContext(ctx).Table(table).Where("deleted_at IS NULL")

	if params.Search != "" {
		escaped := escapeLike(params.Search)
		pattern := "%" + escaped + "%"
		if table == "cores" {
			query = query.Where("version ILIKE ? OR name ILIKE ?", pattern, pattern)
		} else {
			query = query.Where("slug ILIKE ? OR name ILIKE ?", pattern, pattern)
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("failed to list extensions")
	}

	var rows []extensionRow
	if err := query.
		Select(fmt.Sprintf("%s AS slug, name, version", slugCol)).
		Order(slugCol + " ASC").
		Limit(params.Limit).
		Offset(params.Offset).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list extensions")
	}

	indexStatus := s.manager.IndexStatus(params.Repo)

	extensions := make([]ExtensionSummary, len(rows))
	for i, row := range rows {
		extensions[i] = ExtensionSummary{
			Slug:    row.Slug,
			Name:    row.Name,
			Version: row.Version,
			Indexed: indexStatus[row.Slug],
		}
	}

	return &ListResponse{
		Total:      int(total),
		Extensions: extensions,
	}, nil
}

// extensionRow is a minimal struct for scanning extension list queries.
type extensionRow struct {
	Slug    string `gorm:"column:slug"`
	Name    string `gorm:"column:name"`
	Version string `gorm:"column:version"`
}

// convertManagerResponse converts a manager.SearchResponse to our MCP SearchResponse.
func convertManagerResponse(results *manager.SearchResponse) *SearchResponse {
	resp := &SearchResponse{
		TotalExtensions: results.Total,
		Extensions:      make([]ExtensionResult, 0, len(results.Results)),
	}

	for _, r := range results.Results {
		ext := ExtensionResult{
			Slug:           r.Slug,
			Name:           r.Name,
			Version:        r.Version,
			ActiveInstalls: r.ActiveInstalls,
			TotalMatches:   r.TotalMatches,
			Matches:        make([]MatchDetail, 0),
		}

		for _, fm := range r.Matches {
			for _, m := range fm.Matches {
				ext.Matches = append(ext.Matches, MatchDetail{
					File:    fm.Filename,
					Line:    m.LineNumber,
					Content: m.Line,
					Before:  m.Before,
					After:   m.After,
				})
			}
		}

		resp.TotalMatches += r.TotalMatches
		resp.Extensions = append(resp.Extensions, ext)
	}

	return resp
}

// isValidRepo checks if a repo name is one of the allowed values.
func isValidRepo(repo string) bool {
	switch repo {
	case "plugins", "themes", "cores":
		return true
	}
	return false
}

// escapeLike escapes special characters in LIKE/ILIKE patterns.
// PostgreSQL LIKE treats % and _ as wildcards; we escape them so the
// user's search term is matched literally.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
