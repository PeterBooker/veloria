package main

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alecthomas/kong"
)

// invalidPluginSlugs are slugs the index subcommand must refuse for plugins
// and themes: each one either escapes the per-slug directory or collides with
// a name the indexer or the index loader derives from another slug.
var invalidPluginSlugs = []string{
	".", "..", "", "a/b", `a\b`,
	"x.1700000000", "x.staging", "x.old", "x.lock",
	"a\nb", "it's", strings.Repeat("a", 201),
}

var validPluginSlugs = []string{
	"akismet", "wp-super-cache", "my_plugin", "日本語", "%e6%97%a5%e6%9c%ac",
}

// parseCLI parses args with the real command tree, which runs
// IndexCmd.Validate. It works on a copy so the package-level cli is untouched.
func parseCLI(t *testing.T, args []string) (*kong.Context, error) {
	t.Helper()
	c := cli
	parser, err := kong.New(&c, kong.Name("veloria"), kong.Exit(func(int) {}))
	if err != nil {
		t.Fatalf("failed to create parser: %v", err)
	}
	return parser.Parse(args)
}

// indexArgs builds the argument list the server uses when it re-executes
// itself to index one extension.
func indexArgs(repoType, slug, zipURL string) []string {
	return []string{"index", "--repo=" + repoType, "--slug=" + slug, "--zipurl=" + zipURL}
}

// seedDataDir points DATA_DIR at a temp dir holding two already-indexed
// plugins and returns its path.
func seedDataDir(t *testing.T) string {
	t.Helper()
	data := t.TempDir()
	t.Setenv("DATA_DIR", data)
	t.Setenv("ENV", "test") // keeps config.New from looking for a .env file
	for _, slug := range []string{"dummy-a", "dummy-b"} {
		src := filepath.Join(data, "plugins", "source", slug)
		idx := filepath.Join(data, "plugins", "index", slug)
		for _, dir := range []string{src, idx} {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(src, "main.php"), []byte("<?php // original "+slug), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(idx, "trigrams"), []byte("original "+slug+" index"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return data
}

// snapshotTree maps every path under root to its content ("<dir>" for
// directories), so two snapshots are equal only if nothing was added,
// removed or rewritten.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			tree[rel] = "<dir>"
			return nil
		}
		content, err := os.ReadFile(path) // #nosec G304 -- path is under t.TempDir()
		if err != nil {
			return err
		}
		tree[rel] = string(content)
		return nil
	})
	if err != nil {
		t.Fatalf("failed to snapshot %s: %v", root, err)
	}
	return tree
}

// diffTree lists the paths that differ between two snapshots.
func diffTree(before, after map[string]string) []string {
	var changes []string
	for path, content := range before {
		got, ok := after[path]
		switch {
		case !ok:
			changes = append(changes, "removed "+path)
		case got != content:
			changes = append(changes, "changed "+path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			changes = append(changes, "added "+path)
		}
	}
	slices.Sort(changes)
	return changes
}

// zipServer serves a one-plugin archive and counts the requests it receives.
func zipServer(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"pkg/dummy-a/main.php":        "<?php // replaced by archive",
		"pkg/source/dummy-a/main.php": "<?php // replaced by archive",
		"pkg/main.php":                "<?php // archive",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	body := buf.Bytes()

	hits := new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/pkg.zip", hits
}

func TestIndexValidateSlugRule(t *testing.T) {
	tests := []struct {
		repo  string
		slugs []string
		valid bool
	}{
		{"plugins", validPluginSlugs, true},
		{"themes", validPluginSlugs, true},
		{"plugins", invalidPluginSlugs, false},
		{"themes", invalidPluginSlugs, false},
		{"cores", []string{"6.8", "6.8.1"}, true},
		{"cores", []string{".", "..", "", "6", "6.8.1.2", "6.8/..", "6.8\n", "akismet"}, false},
	}
	for _, tt := range tests {
		for _, slug := range tt.slugs {
			cmd := &IndexCmd{Repo: tt.repo, ZipURL: "http://example.com/a.zip", Slug: slug}
			err := cmd.Validate()
			if tt.valid && err != nil {
				t.Errorf("Validate() rejected %s slug %q: %v", tt.repo, slug, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("Validate() accepted %s slug %q", tt.repo, slug)
			}
		}
	}
}

// Slugs such as "." and ".." resolve to the per-type source root or its
// parent, which the indexer would then replace and delete. The command must
// be refused while parsing, before any download or filesystem change.
func TestIndexCommandRefusesInvalidSlugBeforeTouchingDisk(t *testing.T) {
	cases := []struct{ repo, slug string }{
		{"cores", "."},
		{"cores", ".."},
		{"cores", "akismet"},
		{"themes", "."},
		{"themes", ".."},
	}
	for _, slug := range invalidPluginSlugs {
		cases = append(cases, struct{ repo, slug string }{"plugins", slug})
	}

	for _, tc := range cases {
		t.Run(tc.repo+"/"+strings.ToValidUTF8(tc.slug, "?"), func(t *testing.T) {
			data := seedDataDir(t)
			zipURL, hits := zipServer(t)
			before := snapshotTree(t, data)

			ctx, err := parseCLI(t, indexArgs(tc.repo, tc.slug, zipURL))
			if err == nil {
				// Not refused: carry on as main() would, so the damage shows below.
				t.Errorf("slug %q was accepted by the parser; Run returned: %v", tc.slug, ctx.Run())
			} else if !strings.Contains(err.Error(), "invalid slug") {
				t.Errorf("slug %q was refused with an unexpected error: %v", tc.slug, err)
			}

			if changes := diffTree(before, snapshotTree(t, data)); len(changes) != 0 {
				t.Errorf("data dir changed for slug %q: %q", tc.slug, changes)
			}
			if n := hits.Load(); n != 0 {
				t.Errorf("slug %q caused %d download request(s), want 0", tc.slug, n)
			}
		})
	}
}

// IndexCmd.Run must not rely on Validate alone: a slug that does not name a
// direct child of the source and index roots is refused before anything on
// disk is created, renamed or removed.
func TestIndexRunRefusesPathsOutsideRoots(t *testing.T) {
	slugs := []string{
		".", "..", "",
		"dummy-a/..", "dummy-a/sub", "a/../dummy-b",
		"../source/dummy-b", "../index/dummy-b", "../../themes",
	}
	for _, slug := range slugs {
		t.Run(slug, func(t *testing.T) {
			data := seedDataDir(t)
			zipURL, hits := zipServer(t)
			before := snapshotTree(t, data)

			cmd := &IndexCmd{Repo: "plugins", ZipURL: zipURL, Slug: slug}
			if err := cmd.Run(); err == nil {
				t.Errorf("Run() with slug %q succeeded, want an error", slug)
			} else if !strings.Contains(err.Error(), "direct child") {
				t.Errorf("Run() with slug %q failed with an unexpected error: %v", slug, err)
			}

			if changes := diffTree(before, snapshotTree(t, data)); len(changes) != 0 {
				t.Errorf("data dir changed for slug %q: %q", slug, changes)
			}
			if n := hits.Load(); n != 0 {
				t.Errorf("slug %q caused %d download request(s), want 0", slug, n)
			}
		})
	}
}

// Valid slugs still parse, and indexing one touches only its own directories.
func TestIndexCommandAcceptsValidSlug(t *testing.T) {
	for _, slug := range validPluginSlugs {
		for _, repoType := range []string{"plugins", "themes"} {
			if _, err := parseCLI(t, indexArgs(repoType, slug, "http://example.com/a.zip")); err != nil {
				t.Errorf("%s slug %q was refused: %v", repoType, slug, err)
			}
		}
	}
	if _, err := parseCLI(t, indexArgs("cores", "6.8.1", "http://example.com/a.zip")); err != nil {
		t.Errorf("core version 6.8.1 was refused: %v", err)
	}
	// Without --repo the default (plugins) decides which rule applies.
	if _, err := parseCLI(t, []string{"index", "--slug=akismet", "--zipurl=http://example.com/a.zip"}); err != nil {
		t.Errorf("slug akismet was refused for the default repo: %v", err)
	}
	if _, err := parseCLI(t, []string{"index", "--slug=6.8.1", "--zipurl=http://example.com/a.zip"}); err == nil {
		t.Error("slug 6.8.1 was accepted for the default repo, want the plugin rule to refuse it")
	}

}
