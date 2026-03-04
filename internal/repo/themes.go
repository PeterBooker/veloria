package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"veloria/internal/cache"
	"veloria/internal/config"
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

	// Index state tracking (persisted for durable retry)
	RetryCount   int        `json:"-" gorm:"default:0"`
	LastAttemptAt *time.Time `json:"-" gorm:"default:null"`
	IndexedAt    *time.Time `json:"-" gorm:"default:null"`
	IndexStatus  string     `json:"-" gorm:"default:'pending'"`

	LastUpdated time.Time `json:"-" gorm:"-"`
}

// Implement Extension interface
func (t *Theme) GetSlug() string         { return t.Slug }
func (t *Theme) GetSource() string       { return t.Source }
func (t *Theme) GetName() string         { return html.UnescapeString(t.Name) }
func (t *Theme) GetVersion() string      { return t.Version }
func (t *Theme) GetDownloadLink() string { return html.UnescapeString(t.DownloadLink) }
func (t *Theme) GetActiveInstalls() int  { return t.ActiveInstalls }
func (t *Theme) GetDownloaded() int      { return t.Downloaded }
func (t *Theme) GetIndexedExtension() *IndexedExtension {
	return t.IndexedExtension
}
func (t *Theme) SetIndexedExtension(ext *IndexedExtension) {
	t.IndexedExtension = ext
}

// TableName returns the database table name for GORM.
func (t *Theme) TableName() string { return "themes" }

// ThemeStore manages themes using the generic ExtensionStore.
type ThemeStore struct {
	*ExtensionStore[*Theme]
}

// NewThemeStore creates a new theme store.
func NewThemeStore(ctx context.Context, db *gorm.DB, c *config.Config, l *zap.Logger, ch cache.Cache, api *APIClient) *ThemeStore {
	repo := NewExtensionStore[*Theme](StoreConfig[*Theme]{
		Ctx:           ctx,
		DB:            db,
		Config:        c,
		Logger:        l,
		Cache:         ch,
		API:           api,
		ExtensionType: TypeThemes,
	})

	return &ThemeStore{
		ExtensionStore: repo,
	}
}

// Load loads themes from the database and their indexes.
func (tr *ThemeStore) Load() error {
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
func (tr *ThemeStore) PrepareUpdates() ([]IndexTask, error) {
	fetchFn := func() ([]*Theme, error) {
		if tr.needsFullScan() {
			tr.l.Info("Running full theme discovery scan...")
			themes, err := tr.discoverNewThemes()
			if err != nil {
				return nil, err
			}
			tr.recordFullScan()
			return themes, nil
		}

		themes, err := FetchThemeUpdates(tr.ctx, tr.c, tr.api, tr.l, tr.db)
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
		// Only clear ClosedAt when the extension has an available download.
		if t.DownloadLink != "" {
			t.ClosedAt = nil
		}

		var existing Theme
		if err := db.Where("slug = ? AND source = ?", t.Slug, t.Source).First(&existing).Error; err == nil {
			t.ID = existing.ID
			if existing.ClosedAt != nil && t.ClosedAt == nil {
				tr.l.Info("Theme is now available again", zap.String("slug", t.Slug))
			}
			return db.Save(t).Error
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			return db.Create(t).Error
		} else {
			return err
		}
	}

	return tr.ExtensionStore.PrepareUpdates(fetchFn, saveFn)
}

// needsFullScan checks the datasources table to determine if a full discovery
// scan is due. This survives server restarts, unlike the previous in-memory field.
func (tr *ThemeStore) needsFullScan() bool {
	var lastScan sql.NullTime
	err := tr.db.Table("datasources").
		Where("repo_type = ?", string(TypeThemes)).
		Pluck("last_full_scan_at", &lastScan).Error
	if err != nil || !lastScan.Valid {
		return true
	}
	return time.Since(lastScan.Time) >= FullScanInterval
}

// recordFullScan writes the current time as the last full scan timestamp.
func (tr *ThemeStore) recordFullScan() {
	err := tr.db.Table("datasources").
		Where("repo_type = ?", string(TypeThemes)).
		Update("last_full_scan_at", time.Now()).Error
	if err != nil {
		tr.l.Error("Failed to record full scan timestamp", zap.Error(err))
	}
}

// discoverNewThemes paginates the full AspireCloud theme catalog and returns
// themes not yet known to the system.
func (tr *ThemeStore) discoverNewThemes() ([]*Theme, error) {
	known, err := tr.getAllKnownSlugs()
	if err != nil {
		return nil, err
	}

	tr.l.Info("Starting full theme discovery via API", zap.Int("known", len(known)))

	var result []*Theme
	var skipped, metadataUpdated int

	for page := 1; ; page++ {
		if tr.ctx.Err() != nil {
			return nil, tr.ctx.Err()
		}

		pageURL := fmt.Sprintf("%s?action=query_themes&browse=updated&posts_per_page=100&page=%d", baseThemesURL, page)
		themes, info, err := fetchThemePage(tr.ctx, tr.api, pageURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch theme page %d: %w", page, err)
		}

		if len(themes) == 0 {
			break
		}

		for i := range themes {
			t := themes[i]
			if t.Source == "" {
				t.Source = SourceWordPress
			}
			fillWordPressDownloadLink(&t)
			if _, ok := known[t.Slug]; ok {
				tr.updateMetadata(t.Slug, t.Source, map[string]any{
					"version":           t.Version,
					"rating":            t.Rating,
					"active_installs":   t.ActiveInstalls,
					"downloaded":        t.Downloaded,
					"short_description": t.ShortDescription,
					"requires":          t.Requires,
					"requires_php":      t.RequiresPHP,
					"download_link":     t.DownloadLink,
				})
				metadataUpdated++
				skipped++
				continue
			}
			if t.DownloadLink == "" {
				skipped++
				continue
			}
			t.IndexedExtension = NewIndexedExtension()
			result = append(result, &t)
		}

		if page%10 == 0 {
			tr.l.Info("Theme discovery progress", zap.Int("page", page), zap.Int("totalPages", info.Pages), zap.Int("new", len(result)), zap.Int("skipped", skipped), zap.Int("metadata_updated", metadataUpdated))
		}

		if page >= info.Pages {
			break
		}
	}

	tr.l.Info("Full theme discovery scan complete", zap.Int("known", len(known)), zap.Int("new", len(result)), zap.Int("skipped", skipped), zap.Int("metadata_updated", metadataUpdated))

	return result, nil
}

// getAllKnownSlugs returns a set of all theme slugs known to the system,
// including active, closed, and unindexed themes from both the in-memory
// repository and the database.
func (tr *ThemeStore) getAllKnownSlugs() (map[string]struct{}, error) {
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

// themeResponse represents the JSON response from the WordPress Themes API.
type themeResponse struct {
	Info   pageInfo `json:"info"`
	Themes []Theme  `json:"themes"`
}

const baseThemesURL = "https://api.aspirecloud.net/themes/info/1.2/"

// UnmarshalJSON customizes how we handle fields that sometimes arrive as bool or number.
func (t *Theme) UnmarshalJSON(data []byte) error {
	type Alias Theme
	aux := &struct {
		Version         any    `json:"version"`
		Requires        any    `json:"requires"`
		Tested          any    `json:"tested"`
		RequiresPHP     any    `json:"requires_php"`
		TagsRaw         any    `json:"tags"`
		ReqPluginsRaw   any    `json:"requires_plugins"`
		Downloaded      any    `json:"downloaded"`
		ActiveInstalls  any    `json:"active_installs"`
		Rating          any    `json:"rating"`
		LastUpdatedTime string `json:"last_updated_time"`
		AuthorRaw       any    `json:"author"`
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
	if m, ok := aux.TagsRaw.(map[string]any); ok {
		for k, raw := range m {
			t.Tags[k] = toString(raw)
		}
	}

	// RequiresPlugins: sometimes false or an array of strings
	t.RequiresPlugins = parseStringSlice(aux.ReqPluginsRaw)

	// Author and profile (themes have nested author object, but sometimes false)
	if m, ok := aux.AuthorRaw.(map[string]any); ok {
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
// Uses a persistent watermark to avoid missing updates after outages.
func FetchThemeUpdates(ctx context.Context, c *config.Config, api *APIClient, l *zap.Logger, db *gorm.DB) ([]Theme, error) {
	if c.Env == "production" || c.Env == "staging" {
		watermark := readWatermark(db, TypeThemes)
		themes, err := FetchThemesSince(ctx, api, l, watermark)
		if err != nil {
			return nil, err
		}
		writeWatermark(db, TypeThemes, l)
		return themes, nil
	}
	return FetchLocalThemes(ctx, api)
}

// FetchThemesSince fetches pages of themes sorted by update time and
// collects those updated since the given watermark (with a 2-hour overlap margin).
func FetchThemesSince(ctx context.Context, api *APIClient, l *zap.Logger, since time.Time) ([]Theme, error) {
	threshold := since.Add(-2 * time.Hour)

	var all []Theme
	var parseFailures int
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s?action=query_themes&browse=updated&posts_per_page=100&page=%d", baseThemesURL, page)

		themes, info, err := fetchThemePage(ctx, api, url)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch page %d: %w", page, err)
		}

		if len(themes) == 0 {
			l.Warn("Theme updates page returned 0 themes", zap.Int("page", page))
			break
		}

		for _, t := range themes {
			if t.Source == "" {
				t.Source = SourceWordPress
			}
			fillWordPressDownloadLink(&t)
			ts, ok := parseLastUpdated(t.LastUpdatedRaw)
			if !ok {
				parseFailures++
				l.Warn("Failed to parse theme last_updated time, skipping", zap.String("slug", t.Slug), zap.String("lastUpdatedRaw", t.LastUpdatedRaw))
				continue
			}
			t.LastUpdated = ts
			if t.LastUpdated.Before(threshold) {
				if parseFailures > 0 {
					l.Warn("Total theme time parse failures during update check", zap.Int("count", parseFailures))
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
		l.Warn("Total theme time parse failures during update check", zap.Int("count", parseFailures))
	}
	return all, nil
}

// fetchThemePage fetches a single page of themes from the API.
func fetchThemePage(ctx context.Context, api *APIClient, url string) (themes []Theme, info pageInfo, err error) {
	var tr themeResponse
	if err := api.FetchJSON(ctx, url, &tr); err != nil {
		return nil, pageInfo{}, err
	}

	if tr.Info.Pages > 0 && tr.Info.Page > tr.Info.Pages {
		return nil, pageInfo{}, fmt.Errorf("API returned page %d but only %d pages exist (results: %d)", tr.Info.Page, tr.Info.Pages, tr.Info.Results)
	}

	return tr.Themes, tr.Info, nil
}

// FetchThemeInfo fetches information for a single theme.
func FetchThemeInfo(ctx context.Context, api *APIClient, slug string) (theme *Theme, err error) {
	url := fmt.Sprintf("%s?action=theme_information&request[slug]=%s", baseThemesURL, url.QueryEscape(slug))
	var t Theme
	if err := api.FetchJSON(ctx, url, &t); err != nil {
		return nil, fmt.Errorf("failed to fetch theme info for %s: %w", slug, err)
	}

	if t.Source == "" {
		t.Source = SourceWordPress
	}
	fillWordPressDownloadLink(&t)
	return &t, nil
}
