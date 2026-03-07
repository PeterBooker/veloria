package index

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestValidUTF8IgnoringPartialTrailingRune(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "Valid ASCII",
			data: []byte("hello world"),
			want: true,
		},
		{
			name: "Valid multibyte UTF-8",
			data: []byte("你好，世界"), // Each rune is 3 bytes
			want: true,
		},
		{
			name: "Partial multibyte rune at end",
			data: []byte("hello \xe4\xb8"), // Start of '中' (U+4E2D) but incomplete
			want: true,
		},
		{
			name: "Invalid sequence in the middle",
			data: []byte("abc\xffdef"), // 0xFF is not valid UTF-8
			want: false,
		},
		{
			name: "Single valid rune followed by partial",
			data: []byte("ø\xe2"), // 'ø' is valid, then 0xE2 (start of 3-byte)
			want: true,
		},
		{
			name: "RuneError not at end",
			data: []byte{0xe2, 0x28, 0xa1}, // Invalid 3-byte sequence
			want: false,
		},
		{
			name: "Exactly one complete rune",
			data: []byte("✓"),
			want: true,
		},
		{
			name: "Complete rune followed by invalid start byte",
			data: append([]byte("✓"), 0xff),
			want: false,
		},
		{
			name: "Only one start byte of multi-byte rune",
			data: []byte{0xe2}, // Start of 3-byte rune, incomplete
			want: true,
		},
		{
			name: "Multiple valid runes ending with partial",
			data: append([]byte("✓ø"), 0xe2),
			want: true,
		},
		{
			name: "Only continuation byte",
			data: []byte{0x80}, // Continuation byte without start
			want: false,
		},
		{
			name: "Empty buffer",
			data: []byte{},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validUTF8(tt.data)
			if got != tt.want {
				t.Errorf("validUTF8(%q) = %v; want %v", tt.data, got, tt.want)
			}
		})
	}
}

// Helper to create temporary file with specific content
func createTempFile(t *testing.T, content []byte) string {
	t.Helper()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "testfile")
	err := os.WriteFile(tmpFile, content, 0644)
	if err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	return tmpFile
}

func TestIsTextFile_ValidUTF8(t *testing.T) {
	content := []byte("This is a valid UTF-8 text file.\nこんにちは世界\n")
	filename := createTempFile(t, content)

	ok, err := isTextFile(filename)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Errorf("expected true for valid UTF-8, got false")
	}
}

func TestIsTextFile_InvalidUTF8(t *testing.T) {
	// Include invalid UTF-8 byte sequences
	content := []byte{0xff, 0xfe, 0xfd, 0x00, 0x01, 0x02}
	filename := createTempFile(t, content)

	ok, err := isTextFile(filename)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("expected false for invalid UTF-8, got true")
	}
}

func TestIsTextFile_EmptyFile(t *testing.T) {
	filename := createTempFile(t, []byte{})

	ok, err := isTextFile(filename)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Errorf("expected true for empty file, got false")
	}
}

func TestIsTextFile_ShortUTF8(t *testing.T) {
	content := []byte("Hi")
	filename := createTempFile(t, content)

	ok, err := isTextFile(filename)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Errorf("expected true for short valid UTF-8, got false")
	}
}

func TestIsTextFile_NonexistentFile(t *testing.T) {
	ok, err := isTextFile("nonexistentfile.txt")
	if err == nil {
		t.Fatalf("expected error for nonexistent file, got nil")
	}
	if ok {
		t.Errorf("expected false for nonexistent file, got true")
	}
}

func TestTruncateMatchLine(t *testing.T) {
	fre := regexp.MustCompile(`(?i)target`)

	tests := []struct {
		name       string
		line       string
		wantLen    int
		wantSubstr string
	}{
		{
			name:       "Short line unchanged",
			line:       "short line with target word",
			wantLen:    len("short line with target word"),
			wantSubstr: "target",
		},
		{
			name:       "Exactly 250 chars unchanged",
			line:       strings.Repeat("a", 250),
			wantLen:    250,
			wantSubstr: "",
		},
		{
			name:       "Long line truncated around match",
			line:       strings.Repeat("a", 200) + "target" + strings.Repeat("b", 200),
			wantLen:    200, // truncateRadius * 2
			wantSubstr: "target",
		},
		{
			name:       "Match near start",
			line:       "target" + strings.Repeat("x", 300),
			wantLen:    100, // center=0 → window [0:100]
			wantSubstr: "target",
		},
		{
			name:       "Match near end",
			line:       strings.Repeat("x", 300) + "target",
			wantLen:    106, // truncateRadius before match + 6 chars of "target"
			wantSubstr: "target",
		},
		{
			name:       "Nil regexp truncates from start",
			line:       strings.Repeat("z", 400),
			wantLen:    100, // center=0, so [0:100]
			wantSubstr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var re *regexp.Regexp
			if tt.name != "Nil regexp truncates from start" {
				re = fre
			}
			got := truncateMatchLine(tt.line, re)
			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
			}
			if tt.wantSubstr != "" && !strings.Contains(got, tt.wantSubstr) {
				t.Errorf("result %q does not contain %q", got, tt.wantSubstr)
			}
		})
	}
}

func TestTruncateContextLines(t *testing.T) {
	short := "short line"
	long := strings.Repeat("x", 300)

	lines := []string{short, long, short}
	got := truncateContextLines(lines)

	if got[0] != short {
		t.Errorf("short line was modified: %q", got[0])
	}
	if len(got[1]) != maxLineLen {
		t.Errorf("long line len = %d, want %d", len(got[1]), maxLineLen)
	}
	if got[2] != short {
		t.Errorf("second short line was modified: %q", got[2])
	}
}

func TestValidatePattern(t *testing.T) {
	tests := []struct {
		name    string
		pat     string
		wantErr bool
	}{
		{"simple literal", "exec", false},
		{"escaped parens", `exec\(\)`, false},
		{"bare parens (valid empty group)", "exec()", false},
		{"unmatched close paren", `exec\()`, true},
		{"escaped open paren (valid literal)", `exec\(`, false},
		{"valid regex with alternation", `foo|bar`, false},
		{"valid character class", `[a-z]+`, false},
		{"invalid character class", `[a-`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePattern(tt.pat)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
