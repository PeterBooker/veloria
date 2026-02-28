package mcp

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileRange_PlainText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.php")
	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Read all lines
	lines, total, err := readFileRange(path, 1, 500)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(lines) != 5 {
		t.Errorf("len(lines) = %d, want 5", len(lines))
	}
	if lines[0] != "line1" {
		t.Errorf("lines[0] = %q, want %q", lines[0], "line1")
	}

	// Read a range
	lines, total, err = readFileRange(path, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(lines) != 2 {
		t.Errorf("len(lines) = %d, want 2", len(lines))
	}
	if lines[0] != "line2" || lines[1] != "line3" {
		t.Errorf("lines = %v, want [line2, line3]", lines)
	}

	// Read beyond end
	lines, _, err = readFileRange(path, 10, 5)
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
	gz.Close()
	f.Close()

	lines, total, err := readFileRange(path, 1, 500)
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
	lines, _, err := readFileRange(path, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Errorf("len(lines) = %d, want 3", len(lines))
	}

	// maxLines <= 0 should default to maxReadLines
	lines, _, err = readFileRange(path, 1, 0)
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
