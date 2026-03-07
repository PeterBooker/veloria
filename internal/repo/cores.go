package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"veloria/internal/cache"
	"veloria/internal/config"
)

// Core represents a WordPress core release.
type Core struct {
	*IndexedExtension `gorm:"-" json:"-"`

	ID      uuid.UUID `json:"id" gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	Name    string    `json:"name"`
	Version string    `json:"version"`
	ZipURL  string    `json:"-" gorm:"-"`

	// Index stats
	FileCount    int                            `json:"-" gorm:"default:0"`
	TotalSize    int64                          `json:"-" gorm:"default:0"`
	LargestFiles datatypes.JSONSlice[*FileStat] `json:"-" gorm:"type:jsonb;default:'[]'::jsonb"`

	// Index state tracking (persisted for durable retry)
	RetryCount    int        `json:"-" gorm:"default:0"`
	LastAttemptAt *time.Time `json:"-" gorm:"default:null"`
	IndexedAt     *time.Time `json:"-" gorm:"default:null"`
	IndexStatus   string     `json:"-" gorm:"default:'pending'"`
}

// Implement Extension interface
// Note: Core uses Version as its identifier instead of Slug
func (c *Core) GetSlug() string         { return c.Version }
func (c *Core) GetSource() string       { return SourceWordPress }
func (c *Core) GetName() string         { return html.UnescapeString(c.Name) }
func (c *Core) GetVersion() string      { return c.Version }
func (c *Core) GetDownloadLink() string { return c.ZipURL }
func (c *Core) GetActiveInstalls() int  { return 0 } // Cores don't have install counts
func (c *Core) GetDownloaded() int      { return 0 } // Cores don't have download counts
func (c *Core) GetIndexedExtension() *IndexedExtension {
	return c.IndexedExtension
}
func (c *Core) SetIndexedExtension(ext *IndexedExtension) {
	c.IndexedExtension = ext
}

// TableName returns the database table name for GORM.
func (c *Core) TableName() string { return "cores" }

// CoreStore manages WordPress core releases using the generic ExtensionStore.
type CoreStore struct {
	*ExtensionStore[*Core]
}

// NewCoreStore creates a new core store.
func NewCoreStore(ctx context.Context, db *gorm.DB, c *config.Config, l *zap.Logger, ch cache.Cache, api *APIClient) *CoreStore {
	store := NewExtensionStore[*Core](StoreConfig[*Core]{
		Ctx:           ctx,
		DB:            db,
		Config:        c,
		Logger:        l,
		Cache:         ch,
		API:           api,
		ExtensionType: TypeCores,
	})

	return &CoreStore{
		ExtensionStore: store,
	}
}

// Load loads cores from the database and their indexes.
func (cr *CoreStore) Load() error {
	err := cr.LoadFromDB(func(db *gorm.DB) ([]*Core, error) {
		var cores []Core
		if err := db.Where("deleted_at IS NULL").Find(&cores).Error; err != nil {
			return nil, err
		}

		// Convert to pointers and initialize IndexedExtension
		result := make([]*Core, len(cores))
		for i := range cores {
			c := cores[i]
			c.IndexedExtension = NewIndexedExtension()
			result[i] = &c
		}
		return result, nil
	})
	if err != nil {
		return err
	}

	return cr.LoadIndexes()
}

// PrepareUpdates fetches pending core versions and returns IndexTasks for the shared worker pool.
func (cr *CoreStore) PrepareUpdates() ([]IndexTask, error) {
	fetchFn := func() ([]*Core, error) {
		cores, err := FetchCoreUpdates(cr.ctx, cr.c)
		if err != nil {
			return nil, err
		}

		var toIndex []*Core
		for i := range cores {
			c := cores[i]
			c.IndexedExtension = NewIndexedExtension()

			if cr.isVersionIndexed(c.Version) {
				cr.l.Debug("Skipping already indexed core version", zap.String("version", c.Version))
				continue
			}

			toIndex = append(toIndex, &c)
		}

		sortCoresByVersion(toIndex)

		cr.l.Info("Found core versions to index", zap.Int("toIndex", len(toIndex)), zap.Int("total", len(cores)))
		return toIndex, nil
	}

	saveFn := func(db *gorm.DB, c *Core) error {
		var existing Core
		if err := db.Where("version = ?", c.Version).First(&existing).Error; err == nil {
			c.ID = existing.ID
			return db.Save(c).Error
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			return db.Create(c).Error
		} else {
			return err
		}
	}

	return cr.ExtensionStore.PrepareUpdates(fetchFn, saveFn)
}

// isVersionIndexed checks if a core version has already been indexed.
func (cr *CoreStore) isVersionIndexed(version string) bool {
	if existing, ok := cr.Get(version); ok {
		if ie := existing.GetIndexedExtension(); ie != nil && ie.HasIndex() {
			return true
		}
	}
	return false
}

// sortCoresByVersion sorts cores by version number in ascending order.
// This ensures we process older versions first, allowing resumption.
func sortCoresByVersion(cores []*Core) {
	for i := 0; i < len(cores)-1; i++ {
		for j := i + 1; j < len(cores); j++ {
			if compareVersions(cores[i].Version, cores[j].Version) > 0 {
				cores[i], cores[j] = cores[j], cores[i]
			}
		}
	}
}

// compareVersions compares two WordPress version strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareVersions(a, b string) int {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	// Compare each segment
	maxLen := max(len(partsB), len(partsA))

	for i := range maxLen {
		var numA, numB int
		if i < len(partsA) {
			numA, _ = strconv.Atoi(partsA[i])
		}
		if i < len(partsB) {
			numB, _ = strconv.Atoi(partsB[i])
		}

		if numA < numB {
			return -1
		}
		if numA > numB {
			return 1
		}
	}

	return 0
}

// FetchCoreUpdates fetches core updates based on environment.
func FetchCoreUpdates(ctx context.Context, c *config.Config) ([]Core, error) {
	if c.Env == "production" || c.Env == "staging" {
		reqCtx := ctx
		if c.HTTPHandlerTimeout > 0 {
			var cancel context.CancelFunc
			reqCtx, cancel = context.WithTimeout(ctx, c.HTTPHandlerTimeout)
			defer cancel()
		}
		return FetchWordPressReleaseZips(reqCtx)
	}
	return FetchLocalCores()
}

// stableVersionRe matches only stable release versions (e.g. "3.5", "6.8.1").
var stableVersionRe = regexp.MustCompile(`^\d+\.\d+(\.\d+)?$`)

const stableCheckURL = "https://api.wordpress.org/core/stable-check/1.0/"

// FetchWordPressReleaseZips fetches all stable WordPress release versions from
// the WordPress.org stable-check API and returns Core structs with constructed download URLs.
func FetchWordPressReleaseZips(ctx context.Context) ([]Core, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, stableCheckURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create stable-check request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch stable-check API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("unexpected status %s from stable-check API", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB max
	if err != nil {
		return nil, fmt.Errorf("failed to read stable-check response: %w", err)
	}

	var versions map[string]string
	if err := json.Unmarshal(body, &versions); err != nil {
		return nil, fmt.Errorf("failed to parse stable-check response: %w", err)
	}

	var cores []Core
	for version := range versions {
		if !stableVersionRe.MatchString(version) {
			continue
		}
		cores = append(cores, Core{
			Name:    "WordPress " + version,
			Version: version,
			ZipURL:  fmt.Sprintf(coreZipDownloadURL, version),
		})
	}

	if len(cores) == 0 {
		return nil, errors.New("no stable releases found from stable-check API")
	}

	return cores, nil
}
