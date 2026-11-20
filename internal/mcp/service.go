package mcp

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"gorm.io/gorm"

	"veloria/internal/index"
	"veloria/internal/manager"
	"veloria/internal/repo"
	searchmodel "veloria/internal/search/model"
	"veloria/internal/storage"
	"veloria/internal/telemetry"
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
	// Unlike Search, this bypasses the trigram engine. It shares the file
	// tool slots with ListFiles and ReadFile rather than the search slots.
	GrepFile(ctx context.Context, params GrepFileParams) (*GrepFileResponse, error)
}

const (
	// mcpSearchSlots is how many MCP searches may run on the trigram search
	// engine at once, so MCP tool calls cannot overload it.
	mcpSearchSlots = 3
	// mcpSearchSlotWait bounds how long a search waits for a free slot.
	mcpSearchSlotWait = 10 * time.Second
	// mcpSearchTimeout is the engine deadline web and API searches also use.
	mcpSearchTimeout = 60 * time.Second

	// fileToolSlots is how many list_files, read_file and grep_file calls
	// may read extension source trees at once.
	fileToolSlots = 4
	// fileToolSlotWait bounds how long a file tool call waits for a free slot.
	fileToolSlotWait = 5 * time.Second
)

var (
	errSearchBusy    = errors.New("search queue full, try again later")
	errFileToolsBusy = errors.New("file tools are busy, try again later")
)

var (
	// gzipMagicHeader identifies gzip-compressed files.
	gzipMagicHeader = [2]byte{0x1f, 0x8b}
	// contextLinesTable converts validated context line counts to uint without casting.
	contextLinesTable = [maxContext + 1]uint{0, 1, 2, 3, 4, 5}
)

// slotLimiter bounds how many calls run at once and how long a caller waits
// for a free slot before it is told to retry.
type slotLimiter struct {
	slots chan struct{}
	wait  time.Duration
	busy  error
}

func newSlotLimiter(slots int, wait time.Duration, busy error) *slotLimiter {
	return &slotLimiter{slots: make(chan struct{}, slots), wait: wait, busy: busy}
}

// acquire takes a slot, waiting at most l.wait. It returns l.busy when no slot
// frees up in time and the context error when the caller goes away first.
func (l *slotLimiter) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case l.slots <- struct{}{}:
		return nil
	default:
	}

	timer := time.NewTimer(l.wait)
	defer timer.Stop()

	select {
	case l.slots <- struct{}{}:
		return nil
	case <-timer.C:
		return l.busy
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *slotLimiter) release() {
	<-l.slots
}

func contextLinesToUint(v int) uint {
	return contextLinesTable[clampInt(v, 0, maxContext)]
}

// DirectService implements SearchService using the Manager, DB, and S3 directly.
type DirectService struct {
	manager *manager.Manager
	db      *gorm.DB
	s3      storage.ResultStorage

	// The slots are per service; the application runs a single one.
	searchSlots *slotLimiter
	fileSlots   *slotLimiter
}

// NewDirectService creates a DirectService backed by the application's
// Manager (for search), DB (for search records), and S3 (for result persistence).
func NewDirectService(m *manager.Manager, db *gorm.DB, s3 storage.ResultStorage) *DirectService {
	return &DirectService{
		manager:     m,
		db:          db,
		s3:          s3,
		searchSlots: newSlotLimiter(mcpSearchSlots, mcpSearchSlotWait, errSearchBusy),
		fileSlots:   newSlotLimiter(fileToolSlots, fileToolSlotWait, errFileToolsBusy),
	}
}

func (s *DirectService) Search(ctx context.Context, params SearchParams) (string, *SearchResponse, error) {
	if err := index.ValidatePattern(params.Query); err != nil {
		return "", nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	// The search slot is free again before the results are persisted.
	results, err := s.runSearch(ctx, params)
	if err != nil {
		return "", nil, err
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

// runSearch runs the engine search under a search slot and the search
// deadline. The slot is held only for the engine call, and the deferred
// release also runs when the engine panics.
func (s *DirectService) runSearch(ctx context.Context, params SearchParams) (*manager.SearchResponse, error) {
	if err := s.searchSlots.acquire(ctx); err != nil {
		return nil, err
	}
	defer s.searchSlots.release()

	searchCtx, cancel := context.WithTimeout(ctx, mcpSearchTimeout)
	defer cancel()

	searchStart := time.Now()
	results, err := s.manager.Search(searchCtx, params.Repo, params.Query, &manager.SearchParams{
		FileMatch:        params.FileMatch,
		ExcludeFileMatch: params.ExcludeFileMatch,
		CaseInsensitive:  !params.CaseSensitive,
		LinesOfContext:   contextLinesToUint(params.ContextLines),
	})
	searchElapsed := time.Since(searchStart).Seconds()

	attrs := metric.WithAttributes(
		attribute.String("source", params.Repo),
		attribute.String("type", "mcp"),
	)
	telemetry.SearchCount.Add(context.Background(), 1, attrs)
	telemetry.SearchDuration.Record(context.Background(), searchElapsed, attrs)

	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	return results, nil
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

const (
	// maxListFiles caps how many entries one list_files call returns.
	maxListFiles = 5000
	// maxReadLines caps how many lines one read_file call returns.
	maxReadLines = 500
	// maxReadBytes caps how much file content one read_file call returns.
	maxReadBytes = 1 << 20
	// maxScanLineBytes is the longest line the file tools can scan.
	maxScanLineBytes = 1 << 20
	// scanBufferSize is the initial line buffer of a file scan.
	scanBufferSize = 64 << 10
)

func (s *DirectService) ListFiles(ctx context.Context, repoType, slug, pattern string) (*ListFilesResponse, error) {
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

	if err := s.fileSlots.acquire(ctx); err != nil {
		return nil, err
	}
	defer s.fileSlots.release()

	var (
		files []FileEntry
		total int
	)
	err = filepath.WalkDir(slugDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
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

		total++
		if len(files) >= maxListFiles {
			// Past the cap only the count is kept.
			return nil
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
		Slug:      slug,
		Repo:      repoType,
		Total:     total,
		Files:     files,
		Truncated: total > len(files),
	}, nil
}

func (s *DirectService) ReadFile(ctx context.Context, repoType, slug, filePath string, startLine, maxLines int) (*ReadFileResponse, error) {
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

	if err := s.fileSlots.acquire(ctx); err != nil {
		return nil, err
	}
	defer s.fileSlots.release()

	lines, totalLines, more, err := readFileRange(ctx, actualPath, startLine, maxLines)
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
		More:       more,
	}, nil
}

// sourceFile reads an extension source file, transparently decompressing
// gzip-compressed sources.
type sourceFile struct {
	io.Reader
	file *os.File
	gz   *gzip.Reader
}

func openSourceFile(path string) (*sourceFile, error) {
	file, err := os.Open(path) // #nosec G304 -- callers pass paths inside a validated source directory
	if err != nil {
		return nil, err
	}
	src := &sourceFile{Reader: file, file: file}

	// Detect gzip by magic bytes.
	var header [2]byte
	n, _ := io.ReadFull(file, header[:])
	isGzip := n == len(header) && bytes.Equal(header[:], gzipMagicHeader[:])
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, err
	}
	if isGzip {
		gz, err := gzip.NewReader(file)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		src.Reader, src.gz = gz, gz
	}

	return src, nil
}

func (f *sourceFile) Close() error {
	if f.gz != nil {
		_ = f.gz.Close()
	}
	return f.file.Close()
}

// scanReader reads from r up to a byte limit and fails once ctx is done, so a
// scan can neither outgrow its budget nor outlive its caller.
type scanReader struct {
	ctx       context.Context
	r         io.Reader
	remaining int64
}

func (s *scanReader) Read(p []byte) (int, error) {
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	if s.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > s.remaining {
		p = p[:s.remaining]
	}
	n, err := s.r.Read(p)
	s.remaining -= int64(n)
	return n, err
}

// readFileRange reads up to maxLines lines starting at startLine from a file,
// transparently handling gzip-compressed sources. It stops as soon as the
// range is filled or the lines read reach maxReadBytes, and then reports
// more=true instead of scanning the rest of the file. totalLines is the line
// count of the file and is only known when its end was reached (more=false).
func readFileRange(ctx context.Context, path string, startLine, maxLines int) (lines []string, totalLines int, more bool, err error) {
	src, err := openSourceFile(path)
	if err != nil {
		return nil, 0, false, err
	}
	defer func() { _ = src.Close() }()

	if maxLines <= 0 || maxLines > maxReadLines {
		maxLines = maxReadLines
	}
	if startLine < 1 {
		startLine = 1
	}

	scanner := bufio.NewScanner(&scanReader{ctx: ctx, r: src, remaining: math.MaxInt64})
	scanner.Buffer(make([]byte, 0, scanBufferSize), maxScanLineBytes) // handle long lines

	var (
		lineNum int
		size    int
	)
	for scanner.Scan() {
		lineNum++
		if lineNum < startLine {
			continue
		}

		line := scanner.Bytes()

		// A line past the requested range or the byte budget shows there is
		// more to read. The first line is always returned so that a caller
		// paging through the file makes progress.
		if len(lines) == maxLines || (len(lines) > 0 && size+len(line) > maxReadBytes) {
			return lines, 0, true, nil
		}

		size += len(line)
		lines = append(lines, string(line))
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, false, err
	}

	return lines, lineNum, false, nil
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
		TotalExtensions: len(results.Results),
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

const (
	// maxGrepMatches caps the matches one grep_file call returns.
	maxGrepMatches = 1000
	// maxGrepFileBytes caps the decompressed bytes grep_file scans in one file.
	maxGrepFileBytes = 8 << 20
	// maxGrepCallBytes caps the decompressed bytes one grep_file call scans.
	maxGrepCallBytes = 128 << 20
	// maxGrepLineLen and grepTruncateRadius mirror maxLineLen and
	// truncateRadius in internal/index, so grep_file returns lines no longer
	// than the trigram search engine does.
	maxGrepLineLen     = 250
	grepTruncateRadius = 100
)

func (s *DirectService) GrepFile(ctx context.Context, params GrepFileParams) (*GrepFileResponse, error) {
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

	if err := s.fileSlots.acquire(ctx); err != nil {
		return nil, err
	}
	defer s.fileSlots.release()

	resp := &GrepFileResponse{
		Slug:  params.Slug,
		Repo:  params.Repo,
		Query: params.Query,
	}

	if err := grepTree(ctx, slugDir, params.FileMatch, re, contextLines, maxGrepCallBytes, resp); err != nil {
		return nil, fmt.Errorf("grep failed: %w", err)
	}

	return resp, nil
}

// grepTree greps the files under slugDir whose base name matches the fileMatch
// glob and adds their matches to resp. It reads at most callBytes decompressed
// bytes in total and stops early at the match cap, when that budget is spent,
// or when ctx is done.
func grepTree(ctx context.Context, slugDir, fileMatch string, re *regexp.Regexp, contextLines int, callBytes int64, resp *GrepFileResponse) error {
	buf := make([]byte, 0, scanBufferSize)

	return filepath.WalkDir(slugDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if resp.TotalMatches >= maxGrepMatches {
			return filepath.SkipAll
		}

		relPath, _ := filepath.Rel(slugDir, path)
		relPath = strings.TrimSuffix(relPath, ".gz")

		if fileMatch != "" {
			matched, matchErr := filepath.Match(fileMatch, filepath.Base(relPath))
			if matchErr != nil {
				return fmt.Errorf("invalid glob: %w", matchErr)
			}
			if !matched {
				return nil
			}
		}

		if callBytes <= 0 {
			// Files remain but the scan budget is spent.
			resp.Truncated = true
			return filepath.SkipAll
		}

		limit := min(callBytes, maxGrepFileBytes)
		matches, read, truncated, grepErr := grepSingleFile(ctx, path, re, contextLines, maxGrepMatches-resp.TotalMatches, limit, buf)
		callBytes -= read
		if err := ctx.Err(); err != nil {
			return err
		}
		if grepErr != nil {
			return nil // skip unreadable files
		}
		if truncated {
			resp.Truncated = true
		}
		if len(matches) == 0 {
			return nil
		}

		resp.Files = append(resp.Files, GrepFileMatch{
			Path:    relPath,
			Matches: matches,
		})
		resp.TotalMatches += len(matches)
		return nil
	})
}

// grepSingleFile streams a file and returns up to maxMatches regex matches
// with optional context lines, transparently handling gzip-compressed sources.
// Only the ring of context lines is held in memory, and at most limit
// decompressed bytes are read: read reports how many, and truncated whether
// the file went on past the limit. buf is the scan buffer, which the caller
// reuses across files.
func grepSingleFile(ctx context.Context, path string, re *regexp.Regexp, contextLines, maxMatches int, limit int64, buf []byte) (matches []GrepLineMatch, read int64, truncated bool, err error) {
	src, err := openSourceFile(path)
	if err != nil {
		return nil, 0, false, err
	}
	defer func() { _ = src.Close() }()

	limited := &scanReader{ctx: ctx, r: src, remaining: limit}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(buf[:0], maxScanLineBytes)

	var (
		before  = lineRing{bufs: make([][]byte, contextLines)}
		pending []int // indexes of matches still collecting After lines
		lineNum int
		capped  bool
	)
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()

		if len(pending) > 0 {
			after := string(line[:min(len(line), maxGrepLineLen)])
			open := pending[:0]
			for _, i := range pending {
				matches[i].After = append(matches[i].After, after)
				if len(matches[i].After) < contextLines {
					open = append(open, i)
				}
			}
			pending = open
		}

		if len(matches) < maxMatches {
			if re.Match(line) {
				matches = append(matches, GrepLineMatch{
					Line:    lineNum,
					Content: truncateMatchLine(line, re),
					Before:  before.lines(),
				})
				if contextLines > 0 {
					pending = append(pending, len(matches)-1)
				}
			}
		} else if len(pending) == 0 {
			capped = true
			break
		}

		before.push(line)
	}
	read = limit - limited.remaining
	if err := scanner.Err(); err != nil {
		return nil, read, false, err
	}

	if !capped && limited.remaining <= 0 {
		// The limit was reached; see whether it cut the file short.
		var probe [1]byte
		n, _ := io.ReadFull(src, probe[:])
		truncated = n > 0
	}

	return matches, read, truncated, nil
}

// lineRing holds the last len(bufs) lines of a scan, each cut to
// maxGrepLineLen. It reuses its buffers, so a scan allocates nothing per line.
type lineRing struct {
	bufs [][]byte
	next int // slot the next line is written to
	held int // lines currently held
}

func (r *lineRing) push(line []byte) {
	if len(r.bufs) == 0 {
		return
	}
	r.bufs[r.next] = append(r.bufs[r.next][:0], line[:min(len(line), maxGrepLineLen)]...)
	r.next = (r.next + 1) % len(r.bufs)
	if r.held < len(r.bufs) {
		r.held++
	}
}

// lines returns the held lines, oldest first.
func (r *lineRing) lines() []string {
	if r.held == 0 {
		return nil
	}
	out := make([]string, 0, r.held)
	oldest := (r.next - r.held + len(r.bufs)) % len(r.bufs)
	for i := range r.held {
		out = append(out, string(r.bufs[(oldest+i)%len(r.bufs)]))
	}
	return out
}

// truncateMatchLine cuts a matched line the way the trigram search engine
// does: a line over maxGrepLineLen keeps grepTruncateRadius bytes either side
// of where the match starts.
func truncateMatchLine(line []byte, re *regexp.Regexp) string {
	if len(line) <= maxGrepLineLen {
		return string(line)
	}

	center := 0
	if loc := re.FindIndex(line); loc != nil {
		center = loc[0]
	}

	start := max(center-grepTruncateRadius, 0)
	end := min(center+grepTruncateRadius, len(line))

	return string(line[start:end])
}
