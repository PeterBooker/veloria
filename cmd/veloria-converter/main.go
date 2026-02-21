// veloria-converter reads completed searches from a wpdir BoltDB file and
// imports them into veloria's PostgreSQL + S3 storage.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	bolt "go.etcd.io/bbolt"
	"google.golang.org/protobuf/proto"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"veloria/cmd/veloria-converter/wpdirpb"
	"veloria/internal/config"
	searchmodel "veloria/internal/search/model"
	"veloria/internal/storage"
	typespb "veloria/internal/types"
)

const (
	fmtDBString    = "host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=%s connect_timeout=%d"
	s3Timeout     = 30 * time.Second
	maxTermLength = 255
)

var validRepos = map[string]bool{
	"plugins": true,
	"themes":  true,
}

func main() {
	var (
		wpDirDB string
		dryRun  bool
	)
	flag.StringVar(&wpDirDB, "wpdir-db", "", "Path to the wpdir BoltDB file (required)")
	flag.BoolVar(&dryRun, "dry-run", false, "Show what would be imported without writing anything")
	flag.Parse()

	if wpDirDB == "" {
		flag.Usage()
		log.Fatal("--wpdir-db is required")
	}

	// Open wpdir BoltDB read-only.
	boltDB, err := bolt.Open(wpDirDB, 0o600, &bolt.Options{
		ReadOnly: true,
		Timeout:  5 * time.Second,
	})
	if err != nil {
		log.Fatalf("failed to open wpdir db %q: %v", wpDirDB, err)
	}
	defer func() {
		if err := boltDB.Close(); err != nil {
			log.Printf("warning: failed to close wpdir db: %v", err)
		}
	}()

	// Collect all search IDs from the all_dates index.
	searchIDs, err := collectSearchIDs(boltDB)
	if err != nil {
		log.Fatalf("failed to collect search IDs: %v", err)
	}
	log.Printf("found %d searches in wpdir db", len(searchIDs))

	if len(searchIDs) == 0 {
		log.Println("nothing to import")
		return
	}

	var (
		db *gorm.DB
		s3 storage.ResultStorage
	)

	if !dryRun {
		c, err := config.New()
		if err != nil {
			log.Fatalf("failed to load config: %v", err)
		}

		// Connect to veloria PostgreSQL.
		db, err = openDB(c)
		if err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			log.Fatalf("failed to get underlying sql.DB: %v", err)
		}
		defer func() {
			if err := sqlDB.Close(); err != nil {
				log.Printf("warning: failed to close database connection: %v", err)
			}
		}()

		// Initialize S3.
		zl := zerolog.New(os.Stderr).With().Timestamp().Logger()
		s3Client, err := storage.NewS3Client(c, &zl)
		if err != nil {
			log.Fatalf("failed to create S3 client: %v", err)
		}
		if err := s3Client.EnsureBucket(context.Background()); err != nil {
			log.Fatalf("failed to ensure S3 bucket: %v", err)
		}
		s3 = s3Client
	}

	var stats importStats
	bar := newProgress(len(searchIDs))
	for i, wpID := range searchIDs {
		bar.set(i, "")
		if err := importSearch(boltDB, db, s3, wpID, dryRun, &stats, bar); err != nil {
			bar.logf("error importing search %s: %v", wpID, err)
			stats.errors++
		}
	}
	bar.set(len(searchIDs), "done")
	bar.finish()

	log.Printf("import complete: %d imported, %d skipped (not completed), %d skipped (duplicate), %d skipped (invalid), %d errors",
		stats.imported, stats.skippedStatus, stats.skippedDup, stats.skippedInvalid, stats.errors)
}

type importStats struct {
	imported       int
	skippedStatus  int
	skippedDup     int
	skippedInvalid int
	errors         int
}

// progress renders a terminal progress bar on stderr. Log messages print above
// the bar so they don't get overwritten.
type progress struct {
	total   int
	current int
	start   time.Time
	isTTY   bool
}

func newProgress(total int) *progress {
	fi, err := os.Stderr.Stat()
	isTTY := err == nil && fi.Mode()&os.ModeCharDevice != 0
	return &progress{
		total: total,
		start: time.Now(),
		isTTY: isTTY,
	}
}

func (p *progress) set(current int, msg string) {
	p.current = current
	if !p.isTTY {
		return
	}
	pct := float64(p.current) / float64(p.total) * 100
	elapsed := time.Since(p.start).Truncate(time.Second)

	const barWidth = 30
	filled := barWidth * p.current / p.total
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	line := fmt.Sprintf("\r\033[K[%s] %d/%d (%.0f%%) %s", bar, p.current, p.total, pct, elapsed)
	if msg != "" {
		const maxMsg = 50
		if len(msg) > maxMsg {
			msg = msg[:maxMsg-3] + "..."
		}
		line += " " + msg
	}
	fmt.Fprint(os.Stderr, line)
}

// logf prints a message above the progress bar, then redraws the bar.
func (p *progress) logf(format string, args ...any) {
	if p.isTTY {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
	log.Printf(format, args...)
	if p.isTTY {
		p.set(p.current, "")
	}
}

// finish clears the progress bar line.
func (p *progress) finish() {
	if p.isTTY {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
}

// getSearchData navigates to the search_data sub-bucket safely.
func getSearchData(tx *bolt.Tx) (*bolt.Bucket, error) {
	searches := tx.Bucket([]byte("searches"))
	if searches == nil {
		return nil, fmt.Errorf("searches bucket not found")
	}
	data := searches.Bucket([]byte("search_data"))
	if data == nil {
		return nil, fmt.Errorf("search_data bucket not found")
	}
	return data, nil
}

// collectSearchIDs reads all search IDs from the all_dates index bucket.
func collectSearchIDs(boltDB *bolt.DB) ([]string, error) {
	var ids []string
	err := boltDB.View(func(tx *bolt.Tx) error {
		searches := tx.Bucket([]byte("searches"))
		if searches == nil {
			return fmt.Errorf("searches bucket not found")
		}
		allDates := searches.Bucket([]byte("all_dates"))
		if allDates == nil {
			return fmt.Errorf("all_dates bucket not found")
		}
		return allDates.ForEach(func(k, v []byte) error {
			ids = append(ids, string(v))
			return nil
		})
	})
	return ids, err
}

// importSearch reads a single search from BoltDB and imports it into veloria.
func importSearch(boltDB *bolt.DB, db *gorm.DB, s3 storage.ResultStorage, wpID string, dryRun bool, stats *importStats, bar *progress) error {
	// Read and unmarshal the search metadata.
	var wpSearch wpdirpb.Search
	err := boltDB.View(func(tx *bolt.Tx) error {
		data, err := getSearchData(tx)
		if err != nil {
			return err
		}
		raw := data.Get([]byte(wpID))
		if raw == nil {
			return fmt.Errorf("search data not found for %s", wpID)
		}
		return proto.Unmarshal(raw, &wpSearch)
	})
	if err != nil {
		return fmt.Errorf("reading search metadata: %w", err)
	}

	// Only import completed searches.
	if wpSearch.Status != wpdirpb.Search_Completed {
		stats.skippedStatus++
		return nil
	}

	// Validate repo value.
	if !validRepos[wpSearch.Repo] {
		bar.logf("skipping search %s: invalid repo %q", wpID, wpSearch.Repo)
		stats.skippedInvalid++
		return nil
	}

	// Validate term length against DB constraint.
	if len(wpSearch.Input) > maxTermLength {
		bar.logf("skipping search %s: term length %d exceeds %d chars", wpID, len(wpSearch.Input), maxTermLength)
		stats.skippedInvalid++
		return nil
	}

	// Parse timestamps.
	createdAt, err := time.Parse(time.RFC3339, wpSearch.Started)
	if err != nil {
		return fmt.Errorf("parsing started time %q: %w", wpSearch.Started, err)
	}
	var completedAt *time.Time
	if wpSearch.Completed != "" {
		t, err := time.Parse(time.RFC3339, wpSearch.Completed)
		if err != nil {
			return fmt.Errorf("parsing completed time %q: %w", wpSearch.Completed, err)
		}
		completedAt = &t
	}

	bar.set(bar.current, fmt.Sprintf("%q (%s)", wpSearch.Input, wpSearch.Repo))

	if dryRun {
		bar.logf("[dry-run] would import: id=%s term=%q repo=%s private=%v matches=%d created=%s",
			wpID, wpSearch.Input, wpSearch.Repo, wpSearch.Private, wpSearch.Matches, createdAt.Format(time.RFC3339))
		stats.imported++
		return nil
	}

	// Dedup check: skip if (term, repo, created_at) already exists.
	var count int64
	if err := db.Model(&searchmodel.Search{}).
		Where("term = ? AND repo = ? AND created_at = ?", wpSearch.Input, wpSearch.Repo, createdAt).
		Count(&count).Error; err != nil {
		return fmt.Errorf("dedup check failed: %w", err)
	}
	if count > 0 {
		stats.skippedDup++
		return nil
	}

	// Read and unmarshal the summary.
	var wpSummary wpdirpb.Summary
	err = boltDB.View(func(tx *bolt.Tx) error {
		data, err := getSearchData(tx)
		if err != nil {
			return err
		}
		raw := data.Get([]byte(wpID + "_summary"))
		if raw == nil {
			return fmt.Errorf("summary not found for %s", wpID)
		}
		return proto.Unmarshal(raw, &wpSummary)
	})
	if err != nil {
		return fmt.Errorf("reading summary: %w", err)
	}

	// Build veloria SearchResponse from summary + per-slug matches.
	veloriaResp, totalMatches, err := buildSearchResponse(boltDB, wpID, &wpSummary, bar)
	if err != nil {
		return fmt.Errorf("building search response: %w", err)
	}

	// Upload to S3 with timeout.
	newID := uuid.New()
	s3Ctx, s3Cancel := context.WithTimeout(context.Background(), s3Timeout)
	defer s3Cancel()
	size, err := s3.UploadResult(s3Ctx, newID.String(), veloriaResp)
	if err != nil {
		return fmt.Errorf("uploading to S3: %w", err)
	}

	// Insert search record into PostgreSQL.
	totalExtensions := len(wpSummary.GetList())
	search := searchmodel.Search{
		ID:              newID,
		Status:          searchmodel.StatusCompleted,
		Private:         wpSearch.Private,
		Term:            wpSearch.Input,
		Repo:            wpSearch.Repo,
		ResultsSize:     &size,
		TotalMatches:    &totalMatches,
		TotalExtensions: &totalExtensions,
		CreatedAt:       createdAt,
		UpdatedAt:       createdAt,
		CompletedAt:     completedAt,
	}

	if err := db.Create(&search).Error; err != nil {
		// Clean up orphaned S3 object.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if delErr := s3.DeleteResult(cleanupCtx, newID.String()); delErr != nil {
			bar.logf("warning: failed to clean up S3 object for %s: %v", newID, delErr)
		}
		return fmt.Errorf("inserting search record: %w", err)
	}

	bar.logf("imported: wpdir=%s -> veloria=%s term=%q repo=%s matches=%d extensions=%d",
		wpID, newID, wpSearch.Input, wpSearch.Repo, totalMatches, totalExtensions)
	stats.imported++
	return nil
}

// buildSearchResponse reads all per-slug matches from BoltDB in a single
// transaction and assembles a veloria protobuf SearchResponse. It computes the
// accurate total match count from actual match data (wpdir's summary count can
// be inaccurate due to a bug where only the last FileMatch count is stored).
func buildSearchResponse(boltDB *bolt.DB, wpID string, summary *wpdirpb.Summary, bar *progress) (*typespb.SearchResponse, int, error) {
	summaryList := summary.GetList()

	// Read all per-slug matches in a single BoltDB transaction.
	matchesBySlug := make(map[string]*wpdirpb.Matches, len(summaryList))
	err := boltDB.View(func(tx *bolt.Tx) error {
		data, err := getSearchData(tx)
		if err != nil {
			return err
		}
		for slug := range summaryList {
			raw := data.Get([]byte(wpID + "_matches_" + slug))
			if raw == nil {
				bar.logf("warning: no match data stored for search=%s slug=%s (matches will be empty)", wpID, slug)
				continue
			}
			var m wpdirpb.Matches
			if err := proto.Unmarshal(raw, &m); err != nil {
				return fmt.Errorf("unmarshalling matches for slug %q: %w", slug, err)
			}
			matchesBySlug[slug] = &m
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	resp := &typespb.SearchResponse{}
	totalMatches := 0

	for slug, result := range summaryList {
		var matches []*wpdirpb.Match
		if m, ok := matchesBySlug[slug]; ok {
			matches = m.GetList()
		}

		fileMatches := groupMatchesByFile(matches)
		slugTotal := len(matches)
		totalMatches += slugTotal

		sr := &typespb.SearchResult{
			Slug:           result.GetSlug(),
			Name:           result.GetName(),
			Version:        result.GetVersion(),
			ActiveInstalls: int64(result.GetActiveInstalls()),
			Matches:        fileMatches,
			TotalMatches:   int32(min(slugTotal, math.MaxInt32)), // #nosec G115 -- clamped to MaxInt32
		}
		resp.Results = append(resp.Results, sr)
	}

	resp.Total = int32(min(len(resp.Results), math.MaxInt32)) // #nosec G115 -- clamped to MaxInt32
	return resp, totalMatches, nil
}

// groupMatchesByFile converts wpdir's flat match list into veloria's
// hierarchical FileMatch -> Match structure.
func groupMatchesByFile(wpMatches []*wpdirpb.Match) []*typespb.FileMatch {
	if len(wpMatches) == 0 {
		return nil
	}

	// Preserve insertion order with a slice + map.
	order := make([]string, 0)
	byFile := make(map[string][]*typespb.Match)

	for _, m := range wpMatches {
		file := m.GetFile()
		if _, exists := byFile[file]; !exists {
			order = append(order, file)
		}
		byFile[file] = append(byFile[file], &typespb.Match{
			Line:       m.GetLineText(),
			LineNumber: int32(min(m.GetLineNum(), math.MaxInt32)), // #nosec G115 -- clamped to MaxInt32
		})
	}

	result := make([]*typespb.FileMatch, 0, len(order))
	for _, file := range order {
		result = append(result, &typespb.FileMatch{
			Filename: file,
			Matches:  byFile[file],
		})
	}
	return result
}

func openDB(c *config.Config) (*gorm.DB, error) {
	dbLogger := gormlogger.New(log.New(os.Stderr, "\r\n", log.LstdFlags), gormlogger.Config{
		LogLevel:                  gormlogger.Error,
		IgnoreRecordNotFoundError: true,
	})

	dbString := fmt.Sprintf(fmtDBString, c.DBHost, c.DBUser, c.DBPass, c.DBName, c.DBPort, c.DBSSLMode, c.DBTimeZone, c.DBConnectTimeout)
	db, err := gorm.Open(postgres.Open(dbString), &gorm.Config{Logger: dbLogger})
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping failed: %w", err)
	}

	return db, nil
}
