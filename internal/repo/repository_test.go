package repo

import (
	"context"
	"slices"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
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

// An upstream record whose slug is not a plain name must be dropped before it
// is saved, listed or scheduled: the slug becomes a directory name under the
// data dir. Valid records in the same batch are still processed.
func TestPrepareUpdatesSkipsInvalidSlugs(t *testing.T) {
	t.Parallel()

	invalid := []string{
		".", "..", "", "a/b", `a\b`,
		"x.1700000000", "x.staging", "x.old", "x.lock",
		"a\nb", "it's", strings.Repeat("a", 201),
	}
	valid := []string{"akismet", "wp-super-cache", "my_plugin", "日本語", "%e6%97%a5%e6%9c%ac"}

	core, logs := observer.New(zapcore.WarnLevel)
	store := NewExtensionStore(StoreConfig[*Plugin]{
		Ctx:           context.Background(),
		Config:        &config.Config{DataDir: t.TempDir()},
		Logger:        zap.New(core),
		ExtensionType: TypePlugins,
	})
	var ran []string
	store.indexRunner = func(slug, _ string) (*indexerResult, error) {
		ran = append(ran, slug)
		return nil, context.Canceled
	}

	fetchFn := func() ([]*Plugin, error) {
		var batch []*Plugin
		// Invalid records first and last, so a valid one is reached after them.
		for _, slug := range append(append([]string{}, invalid...), append(valid, "../akismet")...) {
			batch = append(batch, &Plugin{
				IndexedExtension: NewIndexedExtension(),
				Slug:             slug,
				DownloadLink:     "https://example.com/pkg.zip",
			})
		}
		return batch, nil
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

	var scheduled []string
	for _, task := range tasks {
		scheduled = append(scheduled, task.Slug)
		_ = task.Run()
	}
	if !slices.Equal(scheduled, valid) {
		t.Errorf("scheduled slugs = %q, want %q", scheduled, valid)
	}
	if !slices.Equal(saved, valid) {
		t.Errorf("saved slugs = %q, want %q", saved, valid)
	}
	if !slices.Equal(ran, valid) {
		t.Errorf("index runner received slugs %q, want %q", ran, valid)
	}
	for _, slug := range append(invalid, "../akismet") {
		if store.Exists(slug) {
			t.Errorf("invalid slug %q was added to the store", slug)
		}
	}
	for _, slug := range valid {
		if !store.Exists(slug) {
			t.Errorf("valid slug %q is missing from the store", slug)
		}
	}

	// Each skipped record is logged at warn level with the repo type and a
	// quoted slug, so a newline in the slug cannot start a new log line.
	skipped := logs.FilterMessage("Skipping extension with invalid slug").All()
	if len(skipped) != len(invalid)+1 {
		t.Fatalf("got %d invalid-slug warnings, want %d", len(skipped), len(invalid)+1)
	}
	var logged []string
	for _, entry := range skipped {
		fields := entry.ContextMap()
		if entry.Level != zapcore.WarnLevel || fields["type"] != "plugins" {
			t.Errorf("unexpected log entry: level=%s fields=%v", entry.Level, fields)
		}
		slug, _ := fields["slug"].(string)
		if strings.ContainsAny(slug, "\n\r") {
			t.Errorf("logged slug contains a raw line break: %q", slug)
		}
		logged = append(logged, slug)
	}
	if !slices.Contains(logged, `"a\nb"`) || !slices.Contains(logged, `"it's"`) || !slices.Contains(logged, `".."`) {
		t.Errorf("logged slugs are not quoted as expected: %q", logged)
	}
}
