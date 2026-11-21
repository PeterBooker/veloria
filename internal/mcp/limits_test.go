package mcp

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"

	"veloria/internal/index"
	"veloria/internal/manager"
	"veloria/internal/repo"
	"veloria/internal/telemetry"
	"veloria/internal/testutil"
)

func TestMain(m *testing.M) {
	// Search and the tool handlers record metrics.
	if err := telemetry.InitMetrics(); err != nil {
		fmt.Fprintln(os.Stderr, "init metrics:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

type searchFunc func(ctx context.Context, term string, opt *index.SearchOptions, fn func(int, int)) ([]*repo.SearchResult, int, error)

// newTestService builds the DirectService the application builds, without a
// database or object storage. Every slug resolves to sourceRoot, the way an
// extension store returns the parent of the per-slug source directories.
func newTestService(t *testing.T, sourceRoot string, search searchFunc) *DirectService {
	t.Helper()

	ds := &testutil.FakeDataSource{
		TypeVal:              repo.TypePlugins,
		SearchFunc:           search,
		ResolveSourceDirFunc: func(string) (string, error) { return sourceRoot, nil },
	}
	m, err := manager.NewTestManager(zap.NewNop(), []repo.DataSource{ds})
	if err != nil {
		t.Fatal(err)
	}

	return NewDirectService(m, nil, nil)
}

// gzipBytes compresses content the way extension sources are stored on disk.
func gzipBytes(t *testing.T, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// writeSource writes a gzip-compressed source file.
func writeSource(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, gzipBytes(t, content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// postMCP sends one JSON-RPC message to the handler with no session header.
func postMCP(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`

func TestHTTPHandler_InitializeKeepsNoSession(t *testing.T) {
	srv := NewMCPServer("veloria-test", "0", newTestService(t, t.TempDir(), nil))
	h := NewHTTPHandler(srv)

	const requests = 200
	var issued, registered int
	for i := range requests {
		w := postMCP(h, initializeBody)
		if w.Code != http.StatusOK {
			t.Fatalf("initialize %d: status = %d, body = %s", i, w.Code, w.Body)
		}

		id := w.Header().Get(server.HeaderKeySessionID)
		if id == "" {
			continue
		}
		issued++
		// A notification can only be addressed to a session the server kept.
		if err := srv.SendNotificationToSpecificClient(id, "notifications/message", nil); !errors.Is(err, server.ErrSessionNotFound) {
			registered++
		}
	}

	if issued != 0 || registered != 0 {
		t.Errorf("%d initialize requests: %d were issued a session ID and %d sessions stayed registered, want 0 and 0",
			requests, issued, registered)
	}
	if err := srv.SendNotificationToSpecificClient("", "notifications/message", nil); !errors.Is(err, server.ErrSessionNotFound) {
		t.Errorf("a session is registered under the empty ID: err = %v", err)
	}
}

func TestHTTPHandler_ServesWithoutSession(t *testing.T) {
	root := t.TempDir()
	writeSource(t, filepath.Join(root, "demo", "demo.php.gz"), []byte("<?php\n// needle here\n"))
	h := NewHTTPHandler(NewMCPServer("veloria-test", "0", newTestService(t, root, nil)))

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "list tools",
			body: `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
			want: `"name":"grep_file"`,
		},
		{
			name: "call tool",
			body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"grep_file","arguments":{"repo":"plugins","slug":"demo","query":"needle"}}}`,
			want: "1 matches across 1 files",
		},
		{
			name: "read resource",
			body: `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"veloria://stats"}}`,
			want: "Repository Statistics",
		},
		{
			name: "get prompt",
			body: `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"check-compatibility","arguments":{"slug":"demo"}}}`,
			want: "get_extension_details",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := postMCP(h, tt.body)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body)
			}
			if id := w.Header().Get(server.HeaderKeySessionID); id != "" {
				t.Errorf("response carries session ID %q, want none", id)
			}
			if body := w.Body.String(); !strings.Contains(body, tt.want) || strings.Contains(body, `"error"`) {
				t.Errorf("body should contain %q and no error, got: %s", tt.want, body)
			}
		})
	}
}

func TestGrepFile_NewlineFixtureAllocation(t *testing.T) {
	// A file of bare newlines is about 1 KiB on disk. Buffering it as lines
	// cost over 40 MiB of allocation; streaming it costs a fixed amount.
	root := t.TempDir()
	writeSource(t, filepath.Join(root, "dense", "data.txt.gz"), bytes.Repeat([]byte("\n"), 512<<10))
	svc := newTestService(t, root, nil)

	const ceiling = 4 << 20

	tests := []struct {
		name         string
		query        string
		contextLines int
		wantMatches  int
	}{
		{"no match", "zzz_no_match", 0, 0},
		{"no match with context", "zzz_no_match", maxContext, 0},
		{"every line matches", "^$", maxContext, maxGrepMatches},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			resp, err := svc.GrepFile(context.Background(), GrepFileParams{
				Repo:         "plugins",
				Slug:         "dense",
				Query:        tt.query,
				ContextLines: tt.contextLines,
			})
			runtime.ReadMemStats(&after)

			if err != nil {
				t.Fatal(err)
			}
			if resp.TotalMatches != tt.wantMatches {
				t.Errorf("total matches = %d, want %d", resp.TotalMatches, tt.wantMatches)
			}
			allocated := after.TotalAlloc - before.TotalAlloc
			t.Logf("allocated %.2f MiB", float64(allocated)/(1<<20))
			if allocated > ceiling {
				t.Errorf("allocated %.1f MiB, want at most %d MiB", float64(allocated)/(1<<20), ceiling>>20)
			}
		})
	}
}

// cancelAfterContext reports cancellation once Err has been consulted limit
// times, which cancels a walk partway through without depending on timing.
type cancelAfterContext struct {
	context.Context
	limit int64
	calls atomic.Int64
	done  chan struct{}
	once  sync.Once
}

func newCancelAfterContext(limit int64) *cancelAfterContext {
	return &cancelAfterContext{Context: context.Background(), limit: limit, done: make(chan struct{})}
}

func (c *cancelAfterContext) Err() error {
	if c.calls.Add(1) <= c.limit {
		return nil
	}
	c.once.Do(func() { close(c.done) })
	return context.Canceled
}

func (c *cancelAfterContext) Done() <-chan struct{} { return c.done }

// checksAfterCancel is how often Err was consulted after it first reported
// cancellation.
func (c *cancelAfterContext) checksAfterCancel() int64 {
	return max(c.calls.Load()-c.limit-1, 0)
}

func TestGrepFile_CancelledContext(t *testing.T) {
	const files = 60

	root := t.TempDir()
	for i := range files {
		writeSource(t, filepath.Join(root, "many", fmt.Sprintf("f%03d.php.gz", i)), []byte("<?php\n// needle here\n"))
	}
	svc := newTestService(t, root, nil)
	params := GrepFileParams{Repo: "plugins", Slug: "many", Query: "needle"}

	t.Run("before the walk", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		resp, err := svc.GrepFile(ctx, params)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled (response: %+v)", err, resp)
		}
	})

	t.Run("during the walk", func(t *testing.T) {
		// Each file costs several context checks, so this cancels the call
		// within its first few files.
		ctx := newCancelAfterContext(12)

		resp, err := svc.GrepFile(ctx, params)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled (response: %+v)", err, resp)
		}
		// Visiting the remaining files would consult the context for each one.
		if late := ctx.checksAfterCancel(); late > 3 {
			t.Errorf("context consulted %d more times after cancellation, want the walk to stop at once", late)
		}
	})
}

func TestReadFile_StopsAfterRequestedRange(t *testing.T) {
	// Cut the compressed stream short: the start of the file still reads
	// back, but reading to the end fails. A read that returns the first lines
	// therefore did not go on to scan the rest of the file.
	var content strings.Builder
	for i := range 200_000 {
		fmt.Fprintf(&content, "line %d of a long file\n", i+1)
	}
	compressed := gzipBytes(t, []byte(content.String()))

	root := t.TempDir()
	path := filepath.Join(root, "demo", "long.txt.gz")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, compressed[:len(compressed)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	svc := newTestService(t, root, nil)

	resp, err := svc.ReadFile(context.Background(), "plugins", "demo", "long.txt", 1, 10)
	if err != nil {
		t.Fatalf("reading the first lines scanned past them: %v", err)
	}
	if resp.StartLine != 1 || resp.EndLine != 10 {
		t.Errorf("lines = %d–%d, want 1–10", resp.StartLine, resp.EndLine)
	}
	if !strings.Contains(resp.Content, "line 10 of a long file") || strings.Contains(resp.Content, "line 11 of") {
		t.Errorf("content should hold lines 1–10, got:\n%s", resp.Content)
	}
	if text := FormatReadFile(resp); !strings.Contains(text, "start_line=11") {
		t.Errorf("should show pagination hint, got:\n%s", text)
	}
}

func TestReadFile_ContentByteCap(t *testing.T) {
	// Ten lines of 400 KiB: two fit in the 1 MiB a call may return.
	line := strings.Repeat("a", 400<<10) + "\n"
	root := t.TempDir()
	writeSource(t, filepath.Join(root, "demo", "wide.txt.gz"), []byte(strings.Repeat(line, 10)))
	svc := newTestService(t, root, nil)

	resp, err := svc.ReadFile(context.Background(), "plugins", "demo", "wide.txt", 1, 500)
	if err != nil {
		t.Fatal(err)
	}
	if resp.EndLine != 2 {
		t.Errorf("end line = %d, want 2", resp.EndLine)
	}
	if len(resp.Content) > 1<<20 {
		t.Errorf("content is %d bytes, want at most 1 MiB", len(resp.Content))
	}

	// Paging on from the hint reaches the following lines.
	resp, err = svc.ReadFile(context.Background(), "plugins", "demo", "wide.txt", 3, 500)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StartLine != 3 || resp.EndLine != 4 {
		t.Errorf("lines = %d–%d, want 3–4", resp.StartLine, resp.EndLine)
	}
}

func TestSearch_EngineContextHasDeadline(t *testing.T) {
	var (
		deadline    time.Time
		hasDeadline bool
	)
	svc := newTestService(t, t.TempDir(), func(ctx context.Context, _ string, _ *index.SearchOptions, _ func(int, int)) ([]*repo.SearchResult, int, error) {
		deadline, hasDeadline = ctx.Deadline()
		return nil, 0, nil
	})

	start := time.Now()
	if _, _, err := svc.Search(context.Background(), SearchParams{Query: "needle", Repo: "plugins"}); err != nil {
		t.Fatal(err)
	}

	if !hasDeadline {
		t.Fatal("the engine was called with a context that has no deadline")
	}
	if limit := deadline.Sub(start); limit <= 0 || limit > 61*time.Second {
		t.Errorf("engine deadline is %s after the call, want about 60s", limit.Round(time.Second))
	}
}

func TestSearch_PanicReleasesSlot(t *testing.T) {
	var panicking atomic.Bool
	panicking.Store(true)
	svc := newTestService(t, t.TempDir(), func(context.Context, string, *index.SearchOptions, func(int, int)) ([]*repo.SearchResult, int, error) {
		if panicking.Load() {
			panic("engine failure")
		}
		return nil, 0, nil
	})
	params := SearchParams{Query: "needle", Repo: "plugins"}

	// More panics than there are slots: a leaked slot would starve the last call.
	for range 8 {
		func() {
			defer func() { _ = recover() }()
			_, _, _ = svc.Search(context.Background(), params)
		}()
	}
	panicking.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := svc.Search(ctx, params); err != nil {
		t.Fatalf("search after recovered panics: %v", err)
	}
}
