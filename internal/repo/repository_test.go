package repo

import (
	"context"
	"testing"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"veloria/internal/config"
)

// The paginated updates feed can repeat a slug within one batch; duplicates
// must collapse to a single IndexTask or concurrent indexers race on disk.
func TestPrepareUpdatesDeduplicatesSlugs(t *testing.T) {
	t.Parallel()

	store := NewExtensionStore(StoreConfig[*Plugin]{
		Ctx:           context.Background(),
		Config:        &config.Config{DataDir: t.TempDir()},
		Logger:        zap.NewNop(),
		ExtensionType: TypePlugins,
	})

	fetchFn := func() ([]*Plugin, error) {
		return []*Plugin{
			{IndexedExtension: NewIndexedExtension(), Slug: "security-ninja", DownloadLink: "https://example.com/security-ninja.1.0.zip"},
			{IndexedExtension: NewIndexedExtension(), Slug: "akismet", DownloadLink: "https://example.com/akismet.zip"},
			{IndexedExtension: NewIndexedExtension(), Slug: "security-ninja", DownloadLink: "https://example.com/security-ninja.1.1.zip"},
		}, nil
	}

	var saved []string
	saveFn := func(_ *gorm.DB, p *Plugin) error {
		saved = append(saved, p.Slug)
		return nil
	}

	tasks, err := store.PrepareUpdates(fetchFn, saveFn)
	if err != nil {
		t.Fatalf("PrepareUpdates failed: %v", err)
	}

	counts := make(map[string]int)
	for _, task := range tasks {
		counts[task.Slug]++
	}
	if len(tasks) != 2 || counts["security-ninja"] != 1 || counts["akismet"] != 1 {
		t.Errorf("expected one task per unique slug, got %v", counts)
	}
	if len(saved) != 2 {
		t.Errorf("expected duplicate slug to be skipped before saving, saved: %v", saved)
	}
}
