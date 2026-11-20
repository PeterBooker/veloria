package mcp

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"veloria/internal/index"
	"veloria/internal/repo"
)

func TestReadFileRange_PlainText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.php")
	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Read all lines
	lines, total, more, err := readFileRange(context.Background(), path, 1, 500)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if more {
		t.Error("more = true, want false for a complete read")
	}
	if len(lines) != 5 {
		t.Errorf("len(lines) = %d, want 5", len(lines))
	}
	if lines[0] != "line1" {
		t.Errorf("lines[0] = %q, want %q", lines[0], "line1")
	}

	// Read a range: the read stops after it, so the total is not counted.
	lines, total, more, err = readFileRange(context.Background(), path, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !more || total != 0 {
		t.Errorf("more = %v, total = %d, want true and 0", more, total)
	}
	if len(lines) != 2 {
		t.Errorf("len(lines) = %d, want 2", len(lines))
	}
	if lines[0] != "line2" || lines[1] != "line3" {
		t.Errorf("lines = %v, want [line2, line3]", lines)
	}

	// Read beyond end
	lines, _, _, err = readFileRange(context.Background(), path, 10, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Errorf("len(lines) = %d, want 0 for offset beyond file", len(lines))
	}
}

func TestReadFileRange_Gzip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.php.gz")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	if _, err := gz.Write([]byte("alpha\nbeta\ngamma\n")); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	lines, total, _, err := readFileRange(context.Background(), path, 1, 500)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(lines) != 3 {
		t.Errorf("len(lines) = %d, want 3", len(lines))
	}
	if lines[1] != "beta" {
		t.Errorf("lines[1] = %q, want %q", lines[1], "beta")
	}
}

func TestReadFileRange_ClampDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// startLine < 1 should default to 1
	lines, _, _, err := readFileRange(context.Background(), path, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Errorf("len(lines) = %d, want 3", len(lines))
	}

	// maxLines <= 0 should default to maxReadLines
	lines, _, _, err = readFileRange(context.Background(), path, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Errorf("len(lines) = %d, want 3", len(lines))
	}
}

func TestFormatNumberedLines(t *testing.T) {
	lines := []string{"<?php", "echo 'hello';", "exit;"}
	text := formatNumberedLines(lines, 1)

	if !strings.Contains(text, "1  <?php") {
		t.Errorf("should number from 1, got:\n%s", text)
	}
	if !strings.Contains(text, "3  exit;") {
		t.Errorf("should number last line, got:\n%s", text)
	}

	// With offset
	text = formatNumberedLines(lines, 98)
	if !strings.Contains(text, " 98  <?php") {
		t.Errorf("should number from 98, got:\n%s", text)
	}
	if !strings.Contains(text, "100  exit;") {
		t.Errorf("should align numbers, got:\n%s", text)
	}
}

func TestFormatNumberedLines_Empty(t *testing.T) {
	text := formatNumberedLines(nil, 1)
	if text != "" {
		t.Errorf("empty lines should return empty string, got: %q", text)
	}
}

func TestGrepSingleFile_PlainText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.php")
	content := "<?php\necho 'hello';\n$wpdb->query($sql);\nexit;\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(`wpdb`)
	matches, _, _, err := grepSingleFile(context.Background(), path, re, 0, maxGrepMatches, maxGrepFileBytes, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1", len(matches))
	}
	if matches[0].Line != 3 {
		t.Errorf("line = %d, want 3", matches[0].Line)
	}
	if !strings.Contains(matches[0].Content, "wpdb") {
		t.Errorf("content = %q, should contain 'wpdb'", matches[0].Content)
	}
}

func TestGrepSingleFile_WithContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.php")
	content := "line1\nline2\ntarget\nline4\nline5\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(`target`)
	matches, _, _, err := grepSingleFile(context.Background(), path, re, 2, maxGrepMatches, maxGrepFileBytes, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1", len(matches))
	}
	m := matches[0]
	if len(m.Before) != 2 || m.Before[0] != "line1" || m.Before[1] != "line2" {
		t.Errorf("before = %v, want [line1, line2]", m.Before)
	}
	if len(m.After) != 2 || m.After[0] != "line4" || m.After[1] != "line5" {
		t.Errorf("after = %v, want [line4, line5]", m.After)
	}
}

func TestGrepSingleFile_Gzip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.php.gz")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	if _, err := gz.Write([]byte("alpha\nbeta\ngamma\n")); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(`beta`)
	matches, _, _, err := grepSingleFile(context.Background(), path, re, 0, maxGrepMatches, maxGrepFileBytes, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1", len(matches))
	}
	if matches[0].Line != 2 {
		t.Errorf("line = %d, want 2", matches[0].Line)
	}
}

func TestGrepSingleFile_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.php")
	if err := os.WriteFile(path, []byte("Hello\nWORLD\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(`(?i)hello`)
	matches, _, _, err := grepSingleFile(context.Background(), path, re, 0, maxGrepMatches, maxGrepFileBytes, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("len(matches) = %d, want 2", len(matches))
	}
}

func TestGrepSingleFile_NoMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.php")
	if err := os.WriteFile(path, []byte("nothing here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(`nonexistent`)
	matches, _, _, err := grepSingleFile(context.Background(), path, re, 0, maxGrepMatches, maxGrepFileBytes, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("len(matches) = %d, want 0", len(matches))
	}
}

// grepAllLines is the reference grep: it holds every line in memory, which is
// what the streaming scan must reproduce without doing so.
func grepAllLines(lines []string, re *regexp.Regexp, contextLines int) []GrepLineMatch {
	var matches []GrepLineMatch
	for i, line := range lines {
		if !re.MatchString(line) {
			continue
		}
		m := GrepLineMatch{Line: i + 1, Content: line}
		if start := max(i-contextLines, 0); start < i {
			m.Before = lines[start:i]
		}
		if end := min(i+1+contextLines, len(lines)); i+1 < end {
			m.After = lines[i+1 : end]
		}
		matches = append(matches, m)
	}
	return matches
}

func TestGrepSingleFile_StreamedContextMatchesBuffered(t *testing.T) {
	// Matches at both ends of the file, next to each other, and closer
	// together than the context window.
	lines := []string{
		"hit one", "a", "b", "hit two", "hit three", "c", "d", "e", "f", "g",
		"h", "hit four", "i", "hit five", "j", "k", "l", "m", "n", "hit six",
	}
	path := filepath.Join(t.TempDir(), "test.php")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, pattern := range []string{"hit", "^[a-n]$", "three|four", "."} {
		re := regexp.MustCompile(pattern)
		for contextLines := 0; contextLines <= maxContext; contextLines++ {
			got, _, truncated, err := grepSingleFile(context.Background(), path, re, contextLines, maxGrepMatches, maxGrepFileBytes, nil)
			if err != nil {
				t.Fatal(err)
			}
			if truncated {
				t.Errorf("pattern %q, context %d: reported as truncated", pattern, contextLines)
			}
			if want := grepAllLines(lines, re, contextLines); !reflect.DeepEqual(got, want) {
				t.Errorf("pattern %q, context %d:\n got %+v\nwant %+v", pattern, contextLines, got, want)
			}
		}
	}
}

func TestGrepSingleFile_TruncatesLongLines(t *testing.T) {
	long := strings.Repeat("x", 3000) + "needle" + strings.Repeat("y", 3000)
	content := strings.Repeat("b", 4000) + "\n" + long + "\n" + strings.Repeat("a", 4000) + "\n"
	path := filepath.Join(t.TempDir(), "min.js")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	matches, _, _, err := grepSingleFile(context.Background(), path, regexp.MustCompile("needle"), 1, maxGrepMatches, maxGrepFileBytes, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1", len(matches))
	}

	m := matches[0]
	if len(m.Content) > maxGrepLineLen || !strings.Contains(m.Content, "needle") {
		t.Errorf("content should be at most %d bytes around the match, got %d bytes: %q", maxGrepLineLen, len(m.Content), m.Content)
	}
	if len(m.Before) != 1 || m.Before[0] != strings.Repeat("b", maxGrepLineLen) {
		t.Errorf("before should be one line cut to %d bytes, got %q", maxGrepLineLen, m.Before)
	}
	if len(m.After) != 1 || m.After[0] != strings.Repeat("a", maxGrepLineLen) {
		t.Errorf("after should be one line cut to %d bytes, got %q", maxGrepLineLen, m.After)
	}
}

func TestGrepSingleFile_ByteLimit(t *testing.T) {
	// Ten 100-byte lines.
	line := "needle " + strings.Repeat("x", 92) + "\n"
	path := filepath.Join(t.TempDir(), "test.php.gz")
	writeSource(t, path, []byte(strings.Repeat(line, 10)))
	re := regexp.MustCompile("needle")

	tests := []struct {
		name          string
		limit         int64
		wantRead      int64
		wantMatches   int
		wantTruncated bool
	}{
		{"cut short", 300, 300, 3, true},
		{"exactly the file", 1000, 1000, 10, false},
		{"larger than the file", 5000, 1000, 10, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches, read, truncated, err := grepSingleFile(context.Background(), path, re, 0, maxGrepMatches, tt.limit, nil)
			if err != nil {
				t.Fatal(err)
			}
			if read != tt.wantRead || len(matches) != tt.wantMatches || truncated != tt.wantTruncated {
				t.Errorf("read = %d, matches = %d, truncated = %v; want %d, %d, %v",
					read, len(matches), truncated, tt.wantRead, tt.wantMatches, tt.wantTruncated)
			}
		})
	}
}

func TestGrepSingleFile_StopsAtMatchCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.php")
	if err := os.WriteFile(path, []byte(strings.Repeat("needle\n", 100)), 0o600); err != nil {
		t.Fatal(err)
	}

	matches, _, truncated, err := grepSingleFile(context.Background(), path, regexp.MustCompile("needle"), 2, 5, maxGrepFileBytes, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 5 || truncated {
		t.Fatalf("len(matches) = %d, truncated = %v; want 5, false", len(matches), truncated)
	}
	if last := matches[4]; last.Line != 5 || len(last.After) != 2 {
		t.Errorf("last match = %+v, want line 5 with 2 lines after", last)
	}
}

func TestGrepTree_CallByteBudget(t *testing.T) {
	// Five files of ten 100-byte lines each.
	line := "needle " + strings.Repeat("x", 92) + "\n"
	dir := t.TempDir()
	for i := range 5 {
		writeSource(t, filepath.Join(dir, fmt.Sprintf("f%d.php.gz", i)), []byte(strings.Repeat(line, 10)))
	}
	re := regexp.MustCompile("needle")

	t.Run("budget spent", func(t *testing.T) {
		resp := &GrepFileResponse{}
		if err := grepTree(context.Background(), dir, "", re, 0, 2500, resp); err != nil {
			t.Fatal(err)
		}
		if !resp.Truncated {
			t.Error("truncated = false, want true")
		}
		// Two whole files and half of the third.
		if resp.TotalMatches != 25 || len(resp.Files) != 3 {
			t.Errorf("matches = %d in %d files, want 25 in 3", resp.TotalMatches, len(resp.Files))
		}
	})

	t.Run("budget spent between files", func(t *testing.T) {
		resp := &GrepFileResponse{}
		if err := grepTree(context.Background(), dir, "", re, 0, 2000, resp); err != nil {
			t.Fatal(err)
		}
		if !resp.Truncated || resp.TotalMatches != 20 {
			t.Errorf("truncated = %v, matches = %d; want true, 20", resp.Truncated, resp.TotalMatches)
		}
	})

	t.Run("within budget", func(t *testing.T) {
		resp := &GrepFileResponse{}
		if err := grepTree(context.Background(), dir, "", re, 0, 5000, resp); err != nil {
			t.Fatal(err)
		}
		if resp.Truncated || resp.TotalMatches != 50 {
			t.Errorf("truncated = %v, matches = %d; want false, 50", resp.Truncated, resp.TotalMatches)
		}
	})

	t.Run("filtered files cost nothing", func(t *testing.T) {
		writeSource(t, filepath.Join(dir, "z.js.gz"), []byte(strings.Repeat(line, 10)))
		resp := &GrepFileResponse{}
		if err := grepTree(context.Background(), dir, "*.js", re, 0, 1000, resp); err != nil {
			t.Fatal(err)
		}
		if resp.Truncated || resp.TotalMatches != 10 {
			t.Errorf("truncated = %v, matches = %d; want false, 10", resp.Truncated, resp.TotalMatches)
		}
	})
}

func TestListFiles_CapsOutput(t *testing.T) {
	const extra = 7

	root := t.TempDir()
	dir := filepath.Join(root, "big")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for i := range maxListFiles + extra {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%05d.php.gz", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	svc := newTestService(t, root, nil)

	resp, err := svc.ListFiles(context.Background(), "plugins", "big", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Files) != maxListFiles || resp.Total != maxListFiles+extra || !resp.Truncated {
		t.Errorf("files = %d, total = %d, truncated = %v; want %d, %d, true",
			len(resp.Files), resp.Total, resp.Truncated, maxListFiles, maxListFiles+extra)
	}
	if text := FormatFileList(resp); !strings.Contains(text, "Listing capped at the first 5,000 files") {
		t.Errorf("should show truncation notice, got tail:\n%s", text[max(len(text)-200, 0):])
	}

	resp, err = svc.ListFiles(context.Background(), "plugins", "big", "f0000*")
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Files) != 10 || resp.Total != 10 || resp.Truncated {
		t.Errorf("filtered: files = %d, total = %d, truncated = %v; want 10, 10, false", len(resp.Files), resp.Total, resp.Truncated)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.ListFiles(ctx, "plugins", "big", ""); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled context: err = %v, want context.Canceled", err)
	}
}

func TestSlotLimiter(t *testing.T) {
	errBusy := errors.New("busy")
	l := newSlotLimiter(2, 20*time.Millisecond, errBusy)

	for range 2 {
		if err := l.acquire(context.Background()); err != nil {
			t.Fatalf("acquire with a free slot: %v", err)
		}
	}

	start := time.Now()
	if err := l.acquire(context.Background()); !errors.Is(err, errBusy) {
		t.Errorf("acquire with no free slot: err = %v, want busy", err)
	}
	if waited := time.Since(start); waited < 20*time.Millisecond {
		t.Errorf("gave up after %s, want the full wait", waited)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("acquire with a cancelled context: err = %v, want context.Canceled", err)
	}

	// A slot released during the wait goes to the waiter.
	l.wait = 5 * time.Second
	go func() {
		time.Sleep(10 * time.Millisecond)
		l.release()
	}()
	if err := l.acquire(context.Background()); err != nil {
		t.Errorf("acquire while a slot is released: %v", err)
	}
}

func TestSearch_BusyWhenSlotsHeld(t *testing.T) {
	const wait = 50 * time.Millisecond

	entered := make(chan struct{}, mcpSearchSlots)
	release := make(chan struct{})
	svc := newTestService(t, t.TempDir(), func(_ context.Context, term string, _ *index.SearchOptions, _ func(int, int)) ([]*repo.SearchResult, int, error) {
		if term == "hold" {
			entered <- struct{}{}
			<-release
		}
		return nil, 0, nil
	})
	svc.searchSlots = newSlotLimiter(mcpSearchSlots, wait, errSearchBusy)

	if mcpSearchSlots < 2 {
		t.Errorf("mcpSearchSlots = %d, want more than one", mcpSearchSlots)
	}

	var wg sync.WaitGroup
	for range mcpSearchSlots {
		wg.Go(func() {
			if _, _, err := svc.Search(context.Background(), SearchParams{Query: "hold", Repo: "plugins"}); err != nil {
				t.Errorf("held search: %v", err)
			}
		})
	}
	for range mcpSearchSlots {
		<-entered
	}

	start := time.Now()
	_, _, err := svc.Search(context.Background(), SearchParams{Query: "other", Repo: "plugins"})
	waited := time.Since(start)
	if !errors.Is(err, errSearchBusy) {
		t.Errorf("search with every slot held: err = %v, want %v", err, errSearchBusy)
	}
	if waited < wait || waited > wait+2*time.Second {
		t.Errorf("gave up after %s, want about the %s wait limit", waited, wait)
	}

	close(release)
	wg.Wait()

	// The slots are free again once the held searches return.
	if _, _, err := svc.Search(context.Background(), SearchParams{Query: "other", Repo: "plugins"}); err != nil {
		t.Errorf("search after the slots were released: %v", err)
	}
}

func TestFileTools_BusyWhenSlotsHeld(t *testing.T) {
	root := t.TempDir()
	writeSource(t, filepath.Join(root, "demo", "demo.php.gz"), []byte("<?php\n// needle here\n"))
	svc := newTestService(t, root, nil)
	svc.fileSlots = newSlotLimiter(fileToolSlots, 20*time.Millisecond, errFileToolsBusy)

	calls := map[string]func() error{
		"list_files": func() error {
			_, err := svc.ListFiles(context.Background(), "plugins", "demo", "")
			return err
		},
		"read_file": func() error {
			_, err := svc.ReadFile(context.Background(), "plugins", "demo", "demo.php", 1, 10)
			return err
		},
		"grep_file": func() error {
			_, err := svc.GrepFile(context.Background(), GrepFileParams{Repo: "plugins", Slug: "demo", Query: "needle"})
			return err
		},
	}

	for range fileToolSlots {
		if err := svc.fileSlots.acquire(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, errFileToolsBusy) {
			t.Errorf("%s with every slot held: err = %v, want %v", name, err, errFileToolsBusy)
		}
	}

	for range fileToolSlots {
		svc.fileSlots.release()
	}
	for name, call := range calls {
		if err := call(); err != nil {
			t.Errorf("%s with free slots: %v", name, err)
		}
	}
	if held := len(svc.fileSlots.slots); held != 0 {
		t.Errorf("%d file slots still held after the calls returned, want 0", held)
	}
}

func TestParseExtensionURI(t *testing.T) {
	tests := []struct {
		uri      string
		wantRepo string
		wantSlug string
		wantErr  bool
	}{
		{"veloria://plugins/woocommerce/info", "plugins", "woocommerce", false},
		{"veloria://themes/twentytwentyfour/info", "themes", "twentytwentyfour", false},
		{"veloria://cores/6.7.1/info", "cores", "6.7.1", false},
		{"veloria://invalid/woo/info", "", "", true},
		{"veloria://plugins//info", "", "", true},
		{"veloria://plugins/woo/other", "", "", true},
		{"https://example.com", "", "", true},
		{"veloria://plugins/woo", "", "", true},
	}

	for _, tt := range tests {
		repo, slug, err := parseExtensionURI(tt.uri)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseExtensionURI(%q): expected error", tt.uri)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseExtensionURI(%q): unexpected error: %v", tt.uri, err)
			continue
		}
		if repo != tt.wantRepo || slug != tt.wantSlug {
			t.Errorf("parseExtensionURI(%q) = (%q, %q), want (%q, %q)", tt.uri, repo, slug, tt.wantRepo, tt.wantSlug)
		}
	}
}
