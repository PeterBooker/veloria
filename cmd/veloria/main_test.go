package main

import (
	"testing"

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
