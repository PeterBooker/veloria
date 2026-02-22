package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"veloria/internal/cache"
	"veloria/internal/config"
)

// SourceWordPress is the source identifier for packages mirrored from wordpress.org.
const SourceWordPress = "wordpress.org"

// Plugin represents a WordPress plugin.
type Plugin struct {
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

	// ClosedAt indicates when this plugin was detected as closed (no download link available)
	ClosedAt *time.Time `json:"closed_at,omitempty" gorm:"default:null"`

	LastUpdated time.Time `json:"-" gorm:"-"`
}

// Implement Extension interface
func (p *Plugin) GetSlug() string         { return p.Slug }
func (p *Plugin) GetSource() string       { return p.Source }
func (p *Plugin) GetName() string         { return p.Name }
func (p *Plugin) GetVersion() string      { return p.Version }
func (p *Plugin) GetDownloadLink() string { return p.DownloadLink }
func (p *Plugin) GetActiveInstalls() int  { return p.ActiveInstalls }
func (p *Plugin) GetDownloaded() int      { return p.Downloaded }
func (p *Plugin) GetIndexedExtension() *IndexedExtension {
	return p.IndexedExtension
}
func (p *Plugin) SetIndexedExtension(ext *IndexedExtension) {
	p.IndexedExtension = ext
}

// TableName returns the database table name for GORM.
func (p *Plugin) TableName() string { return "plugins" }

// PluginStore manages plugins using the generic ExtensionStore.
type PluginStore struct {
	*ExtensionStore[*Plugin]
	lastFullScan time.Time
}

// NewPluginStore creates a new plugin store.
func NewPluginStore(ctx context.Context, db *gorm.DB, c *config.Config, l *zerolog.Logger, ch cache.Cache, api *APIClient) *PluginStore {
	repo := NewExtensionStore[*Plugin](StoreConfig[*Plugin]{
		Ctx:           ctx,
		DB:            db,
		Config:        c,
		Logger:        l,
		Cache:         ch,
		API:           api,
		ExtensionType: TypePlugins,
	})

	return &PluginStore{
		ExtensionStore: repo,
	}
}

// Load loads plugins from the database and their indexes.
func (pr *PluginStore) Load() error {
	err := pr.LoadFromDB(func(db *gorm.DB) ([]*Plugin, error) {
		var plugins []Plugin
		if err := db.Where("deleted_at IS NULL").Find(&plugins).Error; err != nil {
			return nil, err
		}

		// Convert to pointers and initialize IndexedExtension
		result := make([]*Plugin, len(plugins))
		for i := range plugins {
			p := plugins[i]
			p.IndexedExtension = NewIndexedExtension()
			result[i] = &p
		}
		return result, nil
	})
	if err != nil {
		return err
	}

	return pr.LoadIndexes()
}

// PrepareUpdates fetches pending plugins and returns IndexTasks for the shared worker pool.
func (pr *PluginStore) PrepareUpdates() []IndexTask {
	fetchFn := func() ([]*Plugin, error) {
		if pr.lastFullScan.IsZero() || time.Since(pr.lastFullScan) >= FullScanInterval {
			pr.l.Info().Msg("Running full plugin discovery scan...")
			plugins, err := pr.discoverNewPlugins()
			if err != nil {
				return nil, err
			}
			pr.lastFullScan = time.Now()
			return plugins, nil
		}

		plugins, err := FetchPluginUpdates(pr.ctx, pr.c, pr.api, pr.l)
		if err != nil {
			return nil, err
		}

		result := make([]*Plugin, len(plugins))
		for i := range plugins {
			p := plugins[i]
			p.IndexedExtension = NewIndexedExtension()
			result[i] = &p
		}
		return result, nil
	}

	saveFn := func(db *gorm.DB, p *Plugin) error {
		p.ClosedAt = nil

		var existing Plugin
		if err := db.Where("slug = ? AND source = ?", p.Slug, p.Source).First(&existing).Error; err == nil {
			p.ID = existing.ID
			if existing.ClosedAt != nil {
				pr.l.Info().Msgf("Plugin %s is now available again", p.Slug)
			}
			return db.Save(p).Error
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			return db.Create(p).Error
		} else {
			return err
		}
	}

	return pr.ExtensionStore.PrepareUpdates(fetchFn, saveFn)
}

// discoverNewPlugins paginates the full AspireCloud plugin catalog and returns
// plugins not yet known to the system.
func (pr *PluginStore) discoverNewPlugins() ([]*Plugin, error) {
	known, err := pr.getAllKnownSlugs()
	if err != nil {
		return nil, err
	}

	pr.l.Info().Int("known", len(known)).Msg("Starting full plugin discovery via API")

	var result []*Plugin
	var skipped int

	for page := 1; ; page++ {
		if pr.ctx.Err() != nil {
			return nil, pr.ctx.Err()
		}

		pageURL := fmt.Sprintf("%s?action=query_plugins&browse=updated&posts_per_page=100&page=%d", basePluginsURL, page)
		plugins, info, err := fetchPluginPage(pr.ctx, pr.api, pageURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch plugin page %d: %w", page, err)
		}

		if len(plugins) == 0 {
			break
		}

		for i := range plugins {
			p := plugins[i]
			if p.Source == "" {
				p.Source = SourceWordPress
			}
			if _, ok := known[p.Slug]; ok {
				skipped++
				continue
			}
			if p.DownloadLink == "" {
				skipped++
				continue
			}
			p.IndexedExtension = NewIndexedExtension()
			result = append(result, &p)
		}

		if page%10 == 0 {
			pr.l.Info().
				Int("page", page).
				Int("totalPages", info.Pages).
				Int("new", len(result)).
				Int("skipped", skipped).
				Msg("Plugin discovery progress")
		}

		if page >= info.Pages {
			break
		}
	}

	pr.l.Info().
		Int("known", len(known)).
		Int("new", len(result)).
		Int("skipped", skipped).
		Msg("Full plugin discovery scan complete")

	return result, nil
}

// getAllKnownSlugs returns a set of all plugin slugs known to the system,
// including active, closed, and unindexed plugins from both the in-memory
// repository and the database.
func (pr *PluginStore) getAllKnownSlugs() (map[string]struct{}, error) {
	known := make(map[string]struct{})

	pr.mu.RLock()
	for slug := range pr.List {
		known[slug] = struct{}{}
	}
	pr.mu.RUnlock()

	var dbSlugs []string
	if err := pr.db.Table("plugins").Pluck("slug", &dbSlugs).Error; err != nil {
		return nil, fmt.Errorf("failed to load known plugin slugs: %w", err)
	}
	for _, s := range dbSlugs {
		known[s] = struct{}{}
	}

	return known, nil
}


// Info holds pagination info from the API response.
type Info struct {
	Page    int `json:"page"`
	Pages   int `json:"pages"`
	Results int `json:"results"`
}

// PluginResponse represents the JSON response from the WordPress Plugins API.
type PluginResponse struct {
	Info    Info     `json:"info"`
	Plugins []Plugin `json:"plugins"`
}

const basePluginsURL = "https://api.aspirecloud.net/plugins/info/1.2/"

// UnmarshalJSON customizes how we handle fields that sometimes arrive as bool or number.
func (p *Plugin) UnmarshalJSON(data []byte) error {
	type Alias Plugin
	aux := &struct {
		Version        any `json:"version"`
		Requires       any `json:"requires"`
		Tested         any `json:"tested"`
		RequiresPHP    any `json:"requires_php"`
		TagsRaw        any `json:"tags"`
		ReqPluginsRaw  any `json:"requires_plugins"`
		Downloaded     any `json:"downloaded"`
		ActiveInstalls any `json:"active_installs"`
		Rating         any `json:"rating"`
		*Alias
	}{Alias: (*Alias)(p)}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	p.Version = toString(aux.Version)
	p.Requires = toString(aux.Requires)
	p.Tested = toString(aux.Tested)
	p.RequiresPHP = toString(aux.RequiresPHP)
	p.Downloaded = toInt(aux.Downloaded)
	p.ActiveInstalls = toInt(aux.ActiveInstalls)
	p.Rating = toInt(aux.Rating)

	// Tags: sometimes the map values might not be strings
	p.Tags = make(map[string]string)
	if m, ok := aux.TagsRaw.(map[string]any); ok {
		for k, raw := range m {
			p.Tags[k] = toString(raw)
		}
	}

	// RequiresPlugins: sometimes false or an array of strings
	p.RequiresPlugins = parseStringSlice(aux.ReqPluginsRaw)

	return nil
}

// FetchPluginUpdates fetches plugin updates based on environment.
func FetchPluginUpdates(ctx context.Context, c *config.Config, api *APIClient, l *zerolog.Logger) ([]Plugin, error) {
	if c.Env == "production" || c.Env == "staging" {
		return FetchPluginsUpdatedWithinLastHour(ctx, api, l)
	}
	return FetchLocalPlugins(ctx, api)
}

// FetchPluginsUpdatedWithinLastHour fetches pages of plugins sorted by
// update time and collects those updated within the last hour.
func FetchPluginsUpdatedWithinLastHour(ctx context.Context, api *APIClient, l *zerolog.Logger) ([]Plugin, error) {
	threshold := time.Now().UTC().Add(-1 * time.Hour)

	var all []Plugin
	var parseFailures int
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s?action=query_plugins&browse=updated&posts_per_page=100&page=%d", basePluginsURL, page)

		plugins, info, err := fetchPluginPage(ctx, api, url)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch page %d: %w", page, err)
		}

		if len(plugins) == 0 {
			l.Warn().Msgf("Plugin updates page %d returned 0 plugins", page)
			break
		}

		for _, p := range plugins {
			if p.Source == "" {
				p.Source = SourceWordPress
			}
			s := strings.TrimSpace(p.LastUpdatedRaw)
			ts, err := time.Parse("2006-01-02 3:04pm MST", s)
			if err != nil {
				ts, err = time.Parse("2006-01-02", s)
			}
			if err != nil {
				parseFailures++
				l.Warn().
					Str("slug", p.Slug).
					Str("lastUpdatedRaw", s).
					Err(err).
					Msg("Failed to parse plugin last_updated time, skipping")
				continue
			}
			p.LastUpdated = ts.UTC()
			if p.LastUpdated.Before(threshold) {
				if parseFailures > 0 {
					l.Warn().Int("count", parseFailures).Msg("Total plugin time parse failures during update check")
				}
				return all, nil
			}
			all = append(all, p)
		}

		if page >= info.Pages {
			break
		}
	}

	if parseFailures > 0 {
		l.Warn().Int("count", parseFailures).Msg("Total plugin time parse failures during update check")
	}
	return all, nil
}

// fetchPluginPage fetches a single page of plugins from the API.
func fetchPluginPage(ctx context.Context, api *APIClient, url string) (plugins []Plugin, info Info, err error) {
	var pr PluginResponse
	if err := api.FetchJSON(ctx, url, &pr); err != nil {
		return nil, Info{}, err
	}

	if pr.Info.Pages > 0 && pr.Info.Page > pr.Info.Pages {
		return nil, Info{}, fmt.Errorf("API returned page %d but only %d pages exist (results: %d)", pr.Info.Page, pr.Info.Pages, pr.Info.Results)
	}

	return pr.Plugins, pr.Info, nil
}

// FetchPluginInfo fetches information for a single plugin.
func FetchPluginInfo(ctx context.Context, api *APIClient, slug string) (plugin *Plugin, err error) {
	url := fmt.Sprintf("%s?action=plugin_information&request[slug]=%s", basePluginsURL, url.QueryEscape(slug))
	var p Plugin
	if err := api.FetchJSON(ctx, url, &p); err != nil {
		return nil, fmt.Errorf("failed to fetch plugin info for %s: %w", slug, err)
	}

	if p.Source == "" {
		p.Source = SourceWordPress
	}
	return &p, nil
}

// Helper functions for JSON unmarshaling

// toString coerces string|bool|number -> string
func toInt(i any) int {
	switch v := i.(type) {
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}

func toString(i any) string {
	switch v := i.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return ""
	case float64:
		if v == math.Trunc(v) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

// parseStringSlice handles requires_plugins which can be false or []string
func parseStringSlice(raw any) []string {
	switch v := raw.(type) {
	case bool:
		return []string{}
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return []string{}
	}
}
