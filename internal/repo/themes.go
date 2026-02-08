package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"veloria/internal/cache"
	"veloria/internal/config"
	"veloria/internal/index"
)

// Theme represents a WordPress theme.
type Theme struct {
	*IndexedExtension `gorm:"-" json:"-"`

	ID               uuid.UUID                   `json:"id" gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	Name             string                      `json:"name"`
	Slug             string                      `json:"slug"`
	Source           string                      `json:"source" gorm:"default:'wordpress.org'"`
	Version          string                      `json:"version"`
	Author           string                      `json:"author" gorm:"-"`
	AuthorProfile    string                      `json:"author_profile" gorm:"-"`
	Requires         string                      `json:"requires"`
	Tested           string                      `json:"tested"`
	RequiresPHP      string                      `json:"requires_php"`
	RequiresPlugins  datatypes.JSONSlice[string] `json:"requires_plugins" gorm:"column:required_plugins;type:jsonb;default:'[]'::jsonb"`
	Rating           int                         `json:"rating"`
	SupportThreads   int                         `json:"support_threads" gorm:"-"`
	ThreadsResolved  int                         `json:"support_threads_resolved" gorm:"-"`
	ActiveInstalls   int                         `json:"active_installs"`
	Downloaded       int                         `json:"downloaded"`
	LastUpdatedRaw   string                      `json:"last_updated" gorm:"-"`
	Added            string                      `json:"added" gorm:"-"`
	Homepage         string                      `json:"homepage" gorm:"-"`
	ShortDescription string                      `json:"short_description"`
	Description      string                      `json:"description" gorm:"-"`
	DownloadLink     string                      `json:"download_link"`
	Tags             map[string]string           `json:"tags" gorm:"type:jsonb;column:tags;serializer:json;default:'{}'::jsonb"`
	DonateLink       string                      `json:"donate_link" gorm:"-"`

	// Index stats
	FileCount    int                            `json:"-" gorm:"default:0"`
	TotalSize    int64                          `json:"-" gorm:"default:0"`
	LargestFiles datatypes.JSONSlice[*FileStat] `json:"-" gorm:"type:jsonb;default:'[]'::jsonb"`

	// ClosedAt indicates when this theme was detected as closed (no download link available)
	ClosedAt *time.Time `json:"closed_at,omitempty" gorm:"default:null"`

	LastUpdated time.Time `json:"-" gorm:"-"`
}

// Implement Extension interface
func (t *Theme) GetSlug() string         { return t.Slug }
func (t *Theme) GetSource() string       { return t.Source }
func (t *Theme) GetName() string         { return t.Name }
func (t *Theme) GetVersion() string      { return t.Version }
func (t *Theme) GetDownloadLink() string { return t.DownloadLink }
func (t *Theme) GetActiveInstalls() int  { return t.ActiveInstalls }
func (t *Theme) GetIndexedExtension() *IndexedExtension {
	return t.IndexedExtension
}
func (t *Theme) SetIndexedExtension(ext *IndexedExtension) {
	t.IndexedExtension = ext
}

// TableName returns the database table name for GORM.
func (t *Theme) TableName() string { return "themes" }

// ThemeRepo manages themes using the generic Repository.
type ThemeRepo struct {
	*Repository[*Theme]
	c            *config.Config
	lastFullScan time.Time
}

// NewThemeRepo creates a new theme repository.
func NewThemeRepo(ctx context.Context, db *gorm.DB, c *config.Config, l *zerolog.Logger, ch cache.Cache) *ThemeRepo {
	repo := NewRepository[*Theme](RepositoryConfig[*Theme]{
		Ctx:      ctx,
		DB:       db,
		Config:   c,
		Logger:   l,
		Cache:    ch,
		RepoType: RepoThemes,
	})

	return &ThemeRepo{
		Repository: repo,
		c:          c,
	}
}

// Load loads themes from the database and their indexes.
func (tr *ThemeRepo) Load() error {
	err := tr.LoadFromDB(func(db *gorm.DB) ([]*Theme, error) {
		var themes []Theme
		if err := db.Where("deleted_at IS NULL").Find(&themes).Error; err != nil {
			return nil, err
		}

		// Convert to pointers and initialize IndexedExtension
		result := make([]*Theme, len(themes))
		for i := range themes {
			t := themes[i]
			t.IndexedExtension = NewIndexedExtension()
			result[i] = &t
		}
		return result, nil
	})
	if err != nil {
		return err
	}

	return tr.LoadIndexes()
}

// PrepareUpdates fetches pending themes and returns IndexTasks for the shared worker pool.
func (tr *ThemeRepo) PrepareUpdates() []IndexTask {
	fetchFn := func() ([]*Theme, error) {
		if tr.lastFullScan.IsZero() || time.Since(tr.lastFullScan) >= FullScanInterval {
			tr.l.Info().Msg("Running full theme discovery scan...")
			themes, err := tr.discoverNewThemes()
			if err != nil {
				return nil, err
			}
			tr.lastFullScan = time.Now()
			return themes, nil
		}

		themes, err := FetchThemeUpdates(tr.ctx, tr.c, tr.l)
		if err != nil {
			return nil, err
		}

		result := make([]*Theme, len(themes))
		for i := range themes {
			t := themes[i]
			t.IndexedExtension = NewIndexedExtension()
			result[i] = &t
		}
		return result, nil
	}

	saveFn := func(db *gorm.DB, t *Theme) error {
		t.ClosedAt = nil

		var existing Theme
		if err := db.Where("slug = ? AND source = ?", t.Slug, t.Source).First(&existing).Error; err == nil {
			t.ID = existing.ID
			if existing.ClosedAt != nil {
				tr.l.Info().Msgf("Theme %s is now available again", t.Slug)
			}
			return db.Save(t).Error
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			return db.Create(t).Error
		} else {
			return err
		}
	}

	return tr.Repository.PrepareUpdates(fetchFn, saveFn)
}

// discoverNewThemes fetches the complete theme catalog from the WordPress SVN
// repository and uses the WordPress API to fetch metadata for any new slugs not
// already known to the system.
func (tr *ThemeRepo) discoverNewThemes() ([]*Theme, error) {
	// Step 1: Fetch all theme slugs from SVN
	tr.l.Info().Msg("Fetching theme slugs from SVN...")
	svnSlugs, err := fetchSVNSlugs(tr.ctx, svnThemesURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch SVN theme listing: %w", err)
	}
	tr.l.Info().Int("total", len(svnSlugs)).Msg("Fetched theme slugs from SVN")

	// Step 2: Load all known slugs (active, closed, unindexed) from DB + memory
	known, err := tr.getAllKnownSlugs()
	if err != nil {
		return nil, err
	}

	// Step 3: Filter to truly new slugs
	var newSlugs []string
	for _, slug := range svnSlugs {
		if _, ok := known[slug]; !ok {
			newSlugs = append(newSlugs, slug)
		}
	}

	tr.l.Info().
		Int("svnTotal", len(svnSlugs)).
		Int("known", len(known)).
		Int("new", len(newSlugs)).
		Msg("Filtered new theme slugs for discovery")

	if len(newSlugs) == 0 {
		tr.l.Info().Msg("No new theme slugs to discover")
		return nil, nil
	}

	// Step 4: Fetch info for each new slug concurrently
	var (
		mu          sync.Mutex
		result      []*Theme
		closedCount int
		errCount    int
		fetched     int
	)

	g, gCtx := errgroup.WithContext(tr.ctx)
	g.SetLimit(discoveryAPIConcurrency)

	for _, slug := range newSlugs {
		s := slug
		g.Go(func() error {
			time.Sleep(50 * time.Millisecond)

			if gCtx.Err() != nil {
				return gCtx.Err()
			}

			t, fetchErr := tryFetchThemeInfo(gCtx, s)

			if gCtx.Err() != nil {
				return gCtx.Err()
			}

			isClosed := fetchErr == nil && (t == nil || t.DownloadLink == "")
			isActive := fetchErr == nil && t != nil && t.DownloadLink != ""

			if isActive {
				t.IndexedExtension = NewIndexedExtension()
			}

			mu.Lock()
			fetched++

			if fetchErr != nil {
				errCount++
			} else if isClosed {
				closedCount++
			}
			if isActive {
				result = append(result, t)
			}

			if fetched%1000 == 0 {
				tr.l.Info().
					Int("fetched", fetched).
					Int("total", len(newSlugs)).
					Int("active", len(result)).
					Int("closed", closedCount).
					Int("errors", errCount).
					Msg("Theme discovery progress")
			}
			mu.Unlock()

			if fetchErr != nil {
				tr.l.Warn().Err(fetchErr).Str("slug", s).Msg("Failed to fetch theme info, will retry next scan")
			} else if isClosed {
				tr.saveClosedTheme(s, t)
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return nil, fmt.Errorf("theme discovery failed: %w", err)
	}

	tr.l.Info().
		Int("svnTotal", len(svnSlugs)).
		Int("known", len(known)).
		Int("new", len(newSlugs)).
		Int("fetched", fetched).
		Int("active", len(result)).
		Int("closed", closedCount).
		Int("errors", errCount).
		Msg("Full theme SVN scan complete")

	return result, nil
}

// getAllKnownSlugs returns a set of all theme slugs known to the system,
// including active, closed, and unindexed themes from both the in-memory
// repository and the database.
// This is scoped to SVN discovery which only produces wordpress.org packages,
// so source is not included in the key.
func (tr *ThemeRepo) getAllKnownSlugs() (map[string]struct{}, error) {
	known := make(map[string]struct{})

	tr.mu.RLock()
	for slug := range tr.List {
		known[slug] = struct{}{}
	}
	tr.mu.RUnlock()

	var dbSlugs []string
	if err := tr.db.Table("themes").Pluck("slug", &dbSlugs).Error; err != nil {
		return nil, fmt.Errorf("failed to load known theme slugs: %w", err)
	}
	for _, s := range dbSlugs {
		known[s] = struct{}{}
	}

	return known, nil
}

// saveClosedTheme creates or updates a theme record as closed.
// If t is nil (API returned null/false), a minimal record is created.
// Only used during SVN discovery, so source is always SourceWordPress.
func (tr *ThemeRepo) saveClosedTheme(slug string, t *Theme) {
	now := time.Now()

	var existing Theme
	err := tr.db.Where("slug = ? AND source = ?", slug, SourceWordPress).First(&existing).Error
	if err == nil {
		if existing.ClosedAt == nil {
			tr.db.Table("themes").Where("slug = ? AND source = ?", slug, SourceWordPress).Update("closed_at", now)
		}
		return
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		tr.l.Error().Err(err).Str("slug", slug).Msg("Failed to check existing theme")
		return
	}

	record := &Theme{
		Slug:            slug,
		Source:          SourceWordPress,
		Name:            slug,
		ClosedAt:        &now,
		Tags:            make(map[string]string),
		RequiresPlugins: []string{},
	}

	if t != nil {
		record.Name = t.Name
		record.Version = t.Version
		record.Requires = t.Requires
		record.Tested = t.Tested
		record.RequiresPHP = t.RequiresPHP
		record.Rating = t.Rating
		record.ActiveInstalls = t.ActiveInstalls
		record.Downloaded = t.Downloaded
		record.ShortDescription = t.ShortDescription
		record.DownloadLink = t.DownloadLink
		if t.Tags != nil {
			record.Tags = t.Tags
		}
		if t.RequiresPlugins != nil {
			record.RequiresPlugins = t.RequiresPlugins
		}
		if t.Name != "" {
			record.Name = t.Name
		}
	}

	if err := tr.db.Create(record).Error; err != nil {
		tr.l.Error().Err(err).Str("slug", slug).Msg("Failed to create closed theme record")
	}
}


// Search searches all themes and returns results.
func (tr *ThemeRepo) Search(term string, opt *index.SearchOptions) ([]*ThemeSearchResult, error) {
	results, err := tr.Repository.Search(term, opt)
	if err != nil {
		return nil, err
	}

	themeResults := make([]*ThemeSearchResult, len(results))
	for i, r := range results {
		themeResults[i] = &ThemeSearchResult{
			Theme:   r.Extension.(*Theme),
			Matches: r.Matches,
		}
	}
	return themeResults, nil
}

// ThemeSearchResult contains search results for a single theme.
type ThemeSearchResult struct {
	Theme   *Theme
	Matches []*index.FileMatch
}

// ThemeResponse represents the JSON response from the WordPress Themes API.
type ThemeResponse struct {
	Info   Info    `json:"info"`
	Themes []Theme `json:"themes"`
}

const baseThemesURL = "https://api.aspirecloud.net/themes/info/1.2/"

// UnmarshalJSON customizes how we handle fields that sometimes arrive as bool or number.
func (t *Theme) UnmarshalJSON(data []byte) error {
	type Alias Theme
	aux := &struct {
		Version         interface{} `json:"version"`
		Requires        interface{} `json:"requires"`
		Tested          interface{} `json:"tested"`
		RequiresPHP     interface{} `json:"requires_php"`
		TagsRaw         interface{} `json:"tags"`
		ReqPluginsRaw   interface{} `json:"requires_plugins"`
		Downloaded      interface{} `json:"downloaded"`
		ActiveInstalls  interface{} `json:"active_installs"`
		Rating          interface{} `json:"rating"`
		LastUpdatedTime string      `json:"last_updated_time"`
		AuthorRaw       interface{} `json:"author"`
		Sections        struct {
			Description string `json:"description"`
		} `json:"sections"`
		ScreenshotURL string `json:"screenshot_url"`
		ReviewsURL    string `json:"reviews_url"`
		NumRatings    int    `json:"num_ratings"`
		*Alias
	}{Alias: (*Alias)(t)}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	t.Version = toString(aux.Version)
	t.Requires = toString(aux.Requires)
	t.Tested = toString(aux.Tested)
	t.RequiresPHP = toString(aux.RequiresPHP)
	t.Downloaded = toInt(aux.Downloaded)
	t.ActiveInstalls = toInt(aux.ActiveInstalls)
	t.Rating = toInt(aux.Rating)

	// Tags: sometimes the map values might not be strings
	t.Tags = make(map[string]string)
	if m, ok := aux.TagsRaw.(map[string]interface{}); ok {
		for k, raw := range m {
			t.Tags[k] = toString(raw)
		}
	}

	// RequiresPlugins: sometimes false or an array of strings
	t.RequiresPlugins = parseStringSlice(aux.ReqPluginsRaw)

	// Author and profile (themes have nested author object, but sometimes false)
	if m, ok := aux.AuthorRaw.(map[string]interface{}); ok {
		t.Author = toString(m["author"])
		t.AuthorProfile = toString(m["profile"])
	}

	// Description from sections
	t.Description = aux.Sections.Description

	// Parse last updated time
	if parsed, err := time.Parse("2006-01-02 15:04:05", aux.LastUpdatedTime); err == nil {
		t.LastUpdated = parsed
	}

	return nil
}

// fillWordPressDownloadLink sets the download link for wordpress.org themes
// when the API response omits it (e.g., query_themes responses).
// Must only be called for themes with Source == SourceWordPress.
func fillWordPressDownloadLink(t *Theme) {
	if t.Source == SourceWordPress && t.DownloadLink == "" && t.Slug != "" && t.Version != "" {
		t.DownloadLink = fmt.Sprintf("https://downloads.wordpress.org/theme/%s.%s.zip", t.Slug, t.Version)
	}
}

// FetchThemeUpdates fetches theme updates based on environment.
func FetchThemeUpdates(ctx context.Context, c *config.Config, l *zerolog.Logger) ([]Theme, error) {
	if c.Env == "production" || c.Env == "staging" {
		return FetchThemesUpdatedWithinLastHour(ctx, l)
	}
	return FetchLocalThemes(ctx)
}

// FetchThemesUpdatedWithinLastHour fetches pages of themes sorted by
// update time and collects those updated within the last hour.
func FetchThemesUpdatedWithinLastHour(ctx context.Context, l *zerolog.Logger) ([]Theme, error) {
	threshold := time.Now().UTC().Add(-1 * time.Hour)

	var all []Theme
	var parseFailures int
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s?action=query_themes&browse=updated&posts_per_page=100&page=%d", baseThemesURL, page)

		themes, info, err := fetchThemePage(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch page %d: %w", page, err)
		}

		if len(themes) == 0 {
			l.Warn().Msgf("Theme updates page %d returned 0 themes", page)
			break
		}

		for _, t := range themes {
			if t.Source == "" {
				t.Source = SourceWordPress
			}
			fillWordPressDownloadLink(&t)
			s := strings.TrimSpace(t.LastUpdatedRaw)
			ts, err := time.Parse("2006-01-02 3:04pm MST", s)
			if err != nil {
				ts, err = time.Parse("2006-01-02", s)
			}
			if err != nil {
				parseFailures++
				l.Warn().
					Str("slug", t.Slug).
					Str("lastUpdatedRaw", s).
					Err(err).
					Msg("Failed to parse theme last_updated time, skipping")
				continue
			}
			t.LastUpdated = ts.UTC()
			if t.LastUpdated.Before(threshold) {
				if parseFailures > 0 {
					l.Warn().Int("count", parseFailures).Msg("Total theme time parse failures during update check")
				}
				return all, nil
			}
			all = append(all, t)
		}

		if page >= info.Pages {
			break
		}
	}

	if parseFailures > 0 {
		l.Warn().Int("count", parseFailures).Msg("Total theme time parse failures during update check")
	}
	return all, nil
}

// fetchThemePage fetches a single page of themes from the API.
func fetchThemePage(ctx context.Context, url string) (themes []Theme, info Info, err error) {
	var tr ThemeResponse
	if err := fetchWPAPIJSON(ctx, url, &tr); err != nil {
		return nil, Info{}, err
	}

	if tr.Info.Pages > 0 && tr.Info.Page > tr.Info.Pages {
		return nil, Info{}, fmt.Errorf("API returned page %d but only %d pages exist (results: %d)", tr.Info.Page, tr.Info.Pages, tr.Info.Results)
	}

	return tr.Themes, tr.Info, nil
}

// FetchThemeInfo fetches information for a single theme.
func FetchThemeInfo(ctx context.Context, slug string) (theme *Theme, err error) {
	url := fmt.Sprintf("%s?action=theme_information&request[slug]=%s", baseThemesURL, url.QueryEscape(slug))
	var t Theme
	if err := fetchWPAPIJSON(ctx, url, &t); err != nil {
		return nil, fmt.Errorf("failed to fetch theme info for %s: %w", slug, err)
	}

	if t.Source == "" {
		t.Source = SourceWordPress
	}
	fillWordPressDownloadLink(&t)
	return &t, nil
}

// tryFetchThemeInfo fetches a single theme's info from the WordPress API.
// Returns (nil, nil) when the API indicates the theme does not exist (null/false response).
// Returns (nil, err) on network/API errors.
// Returns (theme, nil) on success.
func tryFetchThemeInfo(ctx context.Context, slug string) (*Theme, error) {
	reqURL := fmt.Sprintf("%s?action=theme_information&request[slug]=%s", baseThemesURL, url.QueryEscape(slug))

	// Bypass circuit breaker: bulk discovery expects many "not found" responses
	// which should not trip the breaker that protects the regular API path.
	body, err := doWPAPIRequest(ctx, reqURL)
	if err != nil {
		// 404 means the theme doesn't exist on the API — treat as closed, not an error.
		var apiErr *wpAPIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			return nil, nil
		}
		return nil, err
	}

	trimmed := strings.TrimSpace(string(body))
	if trimmed == "null" || trimmed == "false" || trimmed == "" {
		return nil, nil
	}

	var t Theme
	if err := json.Unmarshal(body, &t); err != nil {
		return nil, fmt.Errorf("failed to decode theme info for %s: %w", slug, err)
	}

	// SVN discovery only finds wordpress.org themes
	t.Source = SourceWordPress
	fillWordPressDownloadLink(&t)
	return &t, nil
}
