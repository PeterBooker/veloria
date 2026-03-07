package mcp

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"veloria/internal/index"
	"veloria/internal/manager"
	"veloria/internal/repo"
	searchmodel "veloria/internal/search/model"
	"veloria/internal/storage"
	typespb "veloria/internal/types"
	"veloria/internal/web"
)

// SearchService abstracts data access for MCP tools.
// DirectService implements it using the Manager directly (for the remote server).
type SearchService interface {
	// Search executes a new code search and persists results.
	// Returns the search ID alongside the response so callers can paginate later.
	Search(ctx context.Context, params SearchParams) (searchID string, resp *SearchResponse, err error)

	// LoadSearch retrieves previously persisted search results by ID.
	LoadSearch(ctx context.Context, searchID string) (*SearchResponse, error)

	// ListExtensions returns a paginated list of extensions.
	ListExtensions(ctx context.Context, params ListParams) (*ListResponse, error)

	// GetExtensionDetails returns full metadata for a single extension.
	GetExtensionDetails(ctx context.Context, repo, slug string) (*ExtensionDetails, error)

	// GetRepoStats returns index statistics for one or all repository types.
	// If repo is empty, returns stats for all types.
	GetRepoStats(ctx context.Context, repo string) ([]RepoStats, error)

	// ListFiles returns the file listing for an extension's source tree.
	ListFiles(ctx context.Context, repo, slug, pattern string) (*ListFilesResponse, error)

	// ReadFile returns lines from a file in an extension's source tree.
	ReadFile(ctx context.Context, repo, slug, path string, startLine, maxLines int) (*ReadFileResponse, error)

	// GrepFile searches within a single extension's source files using regex.
	// Unlike Search, this bypasses the trigram engine and does not acquire
	// the search semaphore.
	GrepFile(ctx context.Context, params GrepFileParams) (*GrepFileResponse, error)
}

// searchSem limits concurrent MCP searches to 1 to prevent concurrent
// MCP tool calls from overloading the trigram search engine.
var searchSem = make(chan struct{}, 1)

var (
	// gzipMagicHeader identifies gzip-compressed files.
	gzipMagicHeader = [2]byte{0x1f, 0x8b}
	// contextLinesTable converts validated context line counts to uint without casting.
	contextLinesTable = [maxContext + 1]uint{0, 1, 2, 3, 4, 5}
)

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

func contextLinesToUint(v int) uint {
	return contextLinesTable[clampInt(v, 0, maxContext)]
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
	if err := index.ValidatePattern(params.Query); err != nil {
		return "", nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	if err := acquireSearchSlot(ctx); err != nil {
		return "", nil, fmt.Errorf("search queue full, try again later")
	}
	defer releaseSearchSlot()

	results, err := s.manager.Search(params.Repo, params.Query, &manager.SearchParams{
		FileMatch:        params.FileMatch,
		ExcludeFileMatch: params.ExcludeFileMatch,
		CaseInsensitive:  !params.CaseSensitive,
		LinesOfContext:   contextLinesToUint(params.ContextLines),
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

func (s *DirectService) GetExtensionDetails(ctx context.Context, repoType, slug string) (*ExtensionDetails, error) {
	if !isValidRepo(repoType) {
		return nil, fmt.Errorf("unknown repo: %s", repoType)
	}

	if repoType == "cores" {
		return s.getCoreDetails(ctx, slug)
	}

	var row detailRow
	err := s.db.WithContext(ctx).Table(repoType).
		Select("slug, name, source, version, short_description, requires, tested, requires_php, rating, active_installs, downloaded, download_link, file_count, total_size").
		Where("slug = ? AND deleted_at IS NULL", slug).
		Scan(&row).Error
	if err != nil {
		return nil, fmt.Errorf("failed to load extension")
	}
	if row.Slug == "" {
		return nil, fmt.Errorf("extension not found: %s", slug)
	}

	indexStatus := s.manager.IndexStatus(repoType)

	return &ExtensionDetails{
		Slug:             row.Slug,
		Name:             row.Name,
		Version:          row.Version,
		Source:           row.Source,
		ShortDescription: row.ShortDescription,
		Requires:         row.Requires,
		Tested:           row.Tested,
		RequiresPHP:      row.RequiresPHP,
		Rating:           row.Rating,
		ActiveInstalls:   row.ActiveInstalls,
		Downloaded:       row.Downloaded,
		DownloadLink:     row.DownloadLink,
		Indexed:          indexStatus[row.Slug],
		FileCount:        row.FileCount,
		TotalSize:        row.TotalSize,
	}, nil
}

func (s *DirectService) getCoreDetails(ctx context.Context, version string) (*ExtensionDetails, error) {
	var row struct {
		Name      string `gorm:"column:name"`
		Version   string `gorm:"column:version"`
		FileCount int    `gorm:"column:file_count"`
		TotalSize int64  `gorm:"column:total_size"`
	}
	err := s.db.WithContext(ctx).Table("cores").
		Select("name, version, file_count, total_size").
		Where("version = ? AND deleted_at IS NULL", version).
		Scan(&row).Error
	if err != nil || row.Version == "" {
		return nil, fmt.Errorf("core release not found: %s", version)
	}

	indexStatus := s.manager.IndexStatus("cores")

	return &ExtensionDetails{
		Slug:      row.Version,
		Name:      row.Name,
		Version:   row.Version,
		Source:    repo.SourceWordPress,
		Indexed:   indexStatus[row.Version],
		FileCount: row.FileCount,
		TotalSize: row.TotalSize,
	}, nil
}

func (s *DirectService) GetRepoStats(_ context.Context, repoType string) ([]RepoStats, error) {
	if repoType != "" {
		if !isValidRepo(repoType) {
			return nil, fmt.Errorf("unknown repo: %s", repoType)
		}
		total, indexed, ok := s.manager.Stats(repoType)
		if !ok {
			return nil, fmt.Errorf("stats unavailable for %s", repoType)
		}
		return []RepoStats{{Repo: repoType, Total: total, Indexed: indexed}}, nil
	}

	var results []RepoStats
	for _, rt := range []string{"plugins", "themes", "cores"} {
		total, indexed, ok := s.manager.Stats(rt)
		if !ok {
			continue
		}
		results = append(results, RepoStats{Repo: rt, Total: total, Indexed: indexed})
	}
	return results, nil
}

func (s *DirectService) ListFiles(_ context.Context, repoType, slug, pattern string) (*ListFilesResponse, error) {
	if !isValidRepo(repoType) {
		return nil, fmt.Errorf("unknown repo: %s", repoType)
	}

	sourceDir, err := s.manager.ResolveSourceDir(repoType, slug)
	if err != nil {
		return nil, fmt.Errorf("extension not found or not indexed: %s", slug)
	}

	// ResolveSourceDir returns the parent (e.g. /data/plugins/source/);
	// scope to the slug-specific subdirectory.
	slugDir := filepath.Join(sourceDir, slug)

	var files []FileEntry
	err = filepath.WalkDir(slugDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		relPath, relErr := filepath.Rel(slugDir, path)
		if relErr != nil {
			return relErr
		}

		// Strip .gz suffix for display — source files are gzip-compressed on disk.
		relPath = strings.TrimSuffix(relPath, ".gz")

		if pattern != "" {
			matched, matchErr := filepath.Match(pattern, filepath.Base(relPath))
			if matchErr != nil {
				return fmt.Errorf("invalid pattern: %w", matchErr)
			}
			if !matched {
				return nil
			}
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}

		files = append(files, FileEntry{Path: relPath, Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	return &ListFilesResponse{
		Slug:  slug,
		Repo:  repoType,
		Total: len(files),
		Files: files,
	}, nil
}

const maxReadLines = 500

func (s *DirectService) ReadFile(_ context.Context, repoType, slug, filePath string, startLine, maxLines int) (*ReadFileResponse, error) {
	if !isValidRepo(repoType) {
		return nil, fmt.Errorf("unknown repo: %s", repoType)
	}

	sourceDir, err := s.manager.ResolveSourceDir(repoType, slug)
	if err != nil {
		return nil, fmt.Errorf("extension not found or not indexed: %s", slug)
	}

	// ResolveSourceDir returns the parent (e.g. /data/plugins/source/);
	// scope to the slug-specific subdirectory.
	slugDir := filepath.Join(sourceDir, slug)

	fullPath, err := web.SafeJoin(slugDir, filePath)
	if err != nil {
		return nil, fmt.Errorf("invalid file path")
	}

	// Source files are stored gzip-compressed; try .gz first.
	actualPath := fullPath + ".gz"
	if _, err := os.Stat(actualPath); err != nil {
		actualPath = fullPath
		if _, err := os.Stat(actualPath); err != nil {
			return nil, fmt.Errorf("file not found: %s", filePath)
		}
	}

	lines, totalLines, err := readFileRange(actualPath, startLine, maxLines)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	endLine := startLine + len(lines) - 1
	if len(lines) == 0 {
		endLine = startLine
	}

	return &ReadFileResponse{
		Slug:       slug,
		Repo:       repoType,
		Path:       filePath,
		TotalLines: totalLines,
		StartLine:  startLine,
		EndLine:    endLine,
		Content:    formatNumberedLines(lines, startLine),
	}, nil
}

// readFileRange reads lines [startLine, startLine+maxLines) from a file,
// transparently handling gzip-compressed sources.
// Returns the lines read and the total line count of the file.
func readFileRange(path string, startLine, maxLines int) ([]string, int, error) {
	file, err := os.Open(path) // #nosec G304 -- path validated by SafeJoin
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()

	var r io.Reader = file

	// Detect gzip by magic bytes.
	var header [2]byte
	n, _ := io.ReadFull(file, header[:])
	isGzip := n == len(header) && bytes.Equal(header[:], gzipMagicHeader[:])
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, 0, err
	}
	if isGzip {
		gz, err := gzip.NewReader(file)
		if err != nil {
			return nil, 0, err
		}
		defer gz.Close()
		r = gz
	}

	if maxLines <= 0 || maxLines > maxReadLines {
		maxLines = maxReadLines
	}
	if startLine < 1 {
		startLine = 1
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // handle long lines

	var lines []string
	lineNum := 0
	endLine := startLine + maxLines

	for scanner.Scan() {
		lineNum++
		if lineNum >= startLine && lineNum < endLine {
			lines = append(lines, scanner.Text())
		}
	}

	return lines, lineNum, scanner.Err()
}

// formatNumberedLines prepends line numbers to each line.
func formatNumberedLines(lines []string, startLine int) string {
	if len(lines) == 0 {
		return ""
	}

	var b strings.Builder
	maxNum := startLine + len(lines) - 1
	width := len(fmt.Sprintf("%d", maxNum))

	for i, line := range lines {
		fmt.Fprintf(&b, "%*d  %s\n", width, startLine+i, line)
	}
	return b.String()
}

// detailRow is used for scanning plugin/theme detail queries.
type detailRow struct {
	Slug             string `gorm:"column:slug"`
	Name             string `gorm:"column:name"`
	Source           string `gorm:"column:source"`
	Version          string `gorm:"column:version"`
	ShortDescription string `gorm:"column:short_description"`
	Requires         string `gorm:"column:requires"`
	Tested           string `gorm:"column:tested"`
	RequiresPHP      string `gorm:"column:requires_php"`
	Rating           int    `gorm:"column:rating"`
	ActiveInstalls   int    `gorm:"column:active_installs"`
	Downloaded       int    `gorm:"column:downloaded"`
	DownloadLink     string `gorm:"column:download_link"`
	FileCount        int    `gorm:"column:file_count"`
	TotalSize        int64  `gorm:"column:total_size"`
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
			// The index stores filenames with a slug prefix (e.g. "woocommerce/file.php").
			// Strip it so paths are relative to the extension root and consistent with read_file.
			filename := fm.Filename
			if _, after, ok := strings.Cut(filename, "/"); ok {
				filename = after
			}
			for _, m := range fm.Matches {
				ext.Matches = append(ext.Matches, MatchDetail{
					File:    filename,
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

const maxGrepMatches = 1000

func (s *DirectService) GrepFile(_ context.Context, params GrepFileParams) (*GrepFileResponse, error) {
	if !isValidRepo(params.Repo) {
		return nil, fmt.Errorf("unknown repo: %s", params.Repo)
	}

	sourceDir, err := s.manager.ResolveSourceDir(params.Repo, params.Slug)
	if err != nil {
		return nil, fmt.Errorf("extension not found or not indexed: %s", params.Slug)
	}
	slugDir := filepath.Join(sourceDir, params.Slug)

	pattern := params.Query
	if !params.CaseSensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}

	contextLines := clampInt(params.ContextLines, 0, maxContext)

	resp := &GrepFileResponse{
		Slug:  params.Slug,
		Repo:  params.Repo,
		Query: params.Query,
	}

	err = filepath.WalkDir(slugDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		if resp.TotalMatches >= maxGrepMatches {
			return filepath.SkipAll
		}

		relPath, _ := filepath.Rel(slugDir, path)
		relPath = strings.TrimSuffix(relPath, ".gz")

		if params.FileMatch != "" {
			matched, matchErr := filepath.Match(params.FileMatch, filepath.Base(relPath))
			if matchErr != nil {
				return fmt.Errorf("invalid glob: %w", matchErr)
			}
			if !matched {
				return nil
			}
		}

		matches, grepErr := grepSingleFile(path, re, contextLines)
		if grepErr != nil {
			return nil // skip unreadable files
		}
		if len(matches) == 0 {
			return nil
		}

		// Enforce global cap.
		remaining := maxGrepMatches - resp.TotalMatches
		if len(matches) > remaining {
			matches = matches[:remaining]
		}

		resp.Files = append(resp.Files, GrepFileMatch{
			Path:    relPath,
			Matches: matches,
		})
		resp.TotalMatches += len(matches)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("grep failed: %w", err)
	}

	return resp, nil
}

// grepSingleFile searches a file for regex matches, transparently handling
// gzip-compressed sources. Returns matches with optional context lines.
func grepSingleFile(path string, re *regexp.Regexp, contextLines int) ([]GrepLineMatch, error) {
	file, err := os.Open(path) // #nosec G304 -- path from WalkDir within validated slugDir
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var r io.Reader = file

	// Detect gzip by magic bytes.
	var header [2]byte
	n, _ := io.ReadFull(file, header[:])
	isGzip := n == len(header) && bytes.Equal(header[:], gzipMagicHeader[:])
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if isGzip {
		gz, err := gzip.NewReader(file)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		r = gz
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var allLines []string
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	var matches []GrepLineMatch
	for i, line := range allLines {
		if !re.MatchString(line) {
			continue
		}

		m := GrepLineMatch{
			Line:    i + 1,
			Content: line,
		}

		if contextLines > 0 {
			start := max(i-contextLines, 0)
			if start < i {
				m.Before = allLines[start:i]
			}

			end := min(i+1+contextLines, len(allLines))
			if i+1 < end {
				m.After = allLines[i+1 : end]
			}
		}

		matches = append(matches, m)
	}

	return matches, nil
}
