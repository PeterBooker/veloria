package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alecthomas/kong"
)

func mustParse(t *testing.T, args []string) *kong.Context {
	t.Helper()
	var c struct {
		Serve   ServeCmd   `cmd:"" default:"withargs" help:"Start the HTTP server (default)."`
		Index   IndexCmd   `cmd:"" help:"Download, extract, and index a single extension."`
		Migrate MigrateCmd `cmd:"" help:"Run database migrations."`
		Version VersionCmd `cmd:"" help:"Print version information."`
	}
	parser, err := kong.New(&c, kong.Name("veloria"), kong.Exit(func(int) {}))
	if err != nil {
		t.Fatalf("failed to create parser: %v", err)
	}
	ctx, err := parser.Parse(args)
	if err != nil {
		t.Fatalf("failed to parse args %v: %v", args, err)
	}
	return ctx
}

func TestBareInvocationDefaultsToServe(t *testing.T) {
	ctx := mustParse(t, []string{})
	if cmd := ctx.Command(); cmd != "serve" {
		t.Errorf("bare invocation: got command %q, want %q", cmd, "serve")
	}
}

func TestExplicitServe(t *testing.T) {
	ctx := mustParse(t, []string{"serve"})
	if cmd := ctx.Command(); cmd != "serve" {
		t.Errorf("explicit serve: got command %q, want %q", cmd, "serve")
	}
}

func TestIndexCommand(t *testing.T) {
	ctx := mustParse(t, []string{"index", "--repo=plugins", "--zipurl=http://example.com/a.zip", "--slug=foo"})
	if cmd := ctx.Command(); cmd != "index" {
		t.Errorf("index: got command %q, want %q", cmd, "index")
	}
}

func TestMigrateCommand(t *testing.T) {
	ctx := mustParse(t, []string{"migrate", "up"})
	if cmd := ctx.Command(); cmd != "migrate <command>" {
		t.Errorf("migrate: got command %q, want %q", cmd, "migrate <command>")
	}
}

func TestMigrateWithArgs(t *testing.T) {
	ctx := mustParse(t, []string{"migrate", "up-to", "20260101000001"})
	if cmd := ctx.Command(); cmd != "migrate <command> <args>" {
		t.Errorf("migrate with args: got command %q, want %q", cmd, "migrate <command> <args>")
	}
}

func TestVersionCommand(t *testing.T) {
	ctx := mustParse(t, []string{"version"})
	if cmd := ctx.Command(); cmd != "version" {
		t.Errorf("version: got command %q, want %q", cmd, "version")
	}
}

func TestExitErrorImplementsExitCoder(t *testing.T) {
	e := &exitError{code: 2, msg: "not found"}

	// Verify it satisfies the interface Kong uses for custom exit codes.
	type exitCoder interface {
		ExitCode() int
	}
	var _ exitCoder = e

	if e.ExitCode() != 2 {
		t.Errorf("ExitCode() = %d, want 2", e.ExitCode())
	}
	if e.Error() != "not found" {
		t.Errorf("Error() = %q, want %q", e.Error(), "not found")
	}
}

func TestParseRetryAfterHeader(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"", downloadDefaultRetryWait},
		{"10", 10 * time.Second},
		{"30", 30 * time.Second},
		{"999", downloadMaxRetryWait}, // capped
		{"-5", downloadDefaultRetryWait},
		{"abc", downloadDefaultRetryWait},
		{"  30  ", 30 * time.Second}, // whitespace trimmed
	}
	for _, tt := range tests {
		got := parseRetryAfterHeader(tt.input)
		if got != tt.want {
			t.Errorf("parseRetryAfterHeader(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestValidateZipMagic(t *testing.T) {
	dir := t.TempDir()

	// Valid zip magic bytes.
	validPath := filepath.Join(dir, "valid.zip")
	if err := os.WriteFile(validPath, []byte{'P', 'K', 0x03, 0x04, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateZipMagic(validPath); err != nil {
		t.Errorf("validateZipMagic(valid) = %v, want nil", err)
	}

	// HTML error page (not a zip).
	htmlPath := filepath.Join(dir, "error.zip")
	if err := os.WriteFile(htmlPath, []byte("<html>Not Found</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateZipMagic(htmlPath); err == nil {
		t.Error("validateZipMagic(html) = nil, want error")
	}

	// Too small file.
	tinyPath := filepath.Join(dir, "tiny.zip")
	if err := os.WriteFile(tinyPath, []byte("PK"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateZipMagic(tinyPath); err == nil {
		t.Error("validateZipMagic(tiny) = nil, want error")
	}
}

func TestDownloadZip_PermanentFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, _, err := downloadZip(srv.URL + "/plugin.zip")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	var exitErr *exitError
	if !errorAs(err, &exitErr) || exitErr.ExitCode() != 2 {
		t.Errorf("expected exitError with code 2, got: %v", err)
	}
}

func TestDownloadZip_InvalidZipContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "<html>Error</html>")
	}))
	defer srv.Close()

	_, _, err := downloadZip(srv.URL + "/plugin.zip")
	if err == nil {
		t.Fatal("expected error for invalid zip content")
	}
	var exitErr *exitError
	if !errorAs(err, &exitErr) || exitErr.ExitCode() != 2 {
		t.Errorf("expected exitError with code 2 for invalid zip, got: %v", err)
	}
}

func TestDownloadZip_429RetriesAndSucceeds(t *testing.T) {
	attempt := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// Return a valid zip (just the magic bytes + minimal content).
		_, _ = w.Write([]byte{'P', 'K', 0x03, 0x04})
	}))
	defer srv.Close()

	path, cleanup, err := downloadZip(srv.URL + "/plugin.zip")
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	defer cleanup()

	if path == "" {
		t.Error("expected non-empty path")
	}
	if attempt != 2 {
		t.Errorf("expected 2 attempts, got %d", attempt)
	}
}

func TestIsGitHubReleaseAsset(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://api.github.com/repos/owner/repo/releases/assets/12345", true},
		{"https://downloads.wordpress.org/plugin/foo.1.0.zip", false},
		{"https://fastly.api.aspirecloud.net/download/plugin/foo.1.0.zip", false},
		{"https://api.github.com/repos/owner/repo/zipball/v1.0", false},
	}
	for _, tt := range tests {
		if got := isGitHubReleaseAsset(tt.url); got != tt.want {
			t.Errorf("isGitHubReleaseAsset(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestDownloadZip_GitHubAcceptHeader(t *testing.T) {
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write([]byte{'P', 'K', 0x03, 0x04})
	}))
	defer srv.Close()

	// Simulate a GitHub release asset URL pattern on the test server.
	// We can't use actual api.github.com, so we test the header-setting logic
	// by checking that non-GitHub URLs do NOT get the Accept header.
	path, cleanup, err := downloadZip(srv.URL + "/plugin.zip")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()
	_ = path

	if gotAccept == "application/octet-stream" {
		t.Error("non-GitHub URL should not send Accept: application/octet-stream")
	}
}

// errorAs is a helper to avoid importing errors in the test file.
func errorAs[T error](err error, target *T) bool {
	for err != nil {
		if e, ok := err.(T); ok {
			*target = e
			return true
		}
		if u, ok := err.(interface{ Unwrap() error }); ok {
			err = u.Unwrap()
		} else {
			return false
		}
	}
	return false
}

func TestIndexValidateRejectsPathSeparators(t *testing.T) {
	cmd := &IndexCmd{Repo: "plugins", ZipURL: "http://example.com/a.zip", Slug: "foo/bar"}
	if err := cmd.Validate(); err == nil {
		t.Error("Validate() should reject slug with path separator")
	}

	cmd.Slug = `foo\bar`
	if err := cmd.Validate(); err == nil {
		t.Error("Validate() should reject slug with backslash")
	}

	cmd.Slug = "valid-slug"
	if err := cmd.Validate(); err != nil {
		t.Errorf("Validate() should accept valid slug, got: %v", err)
	}
}
