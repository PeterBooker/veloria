package index

import (
	"os"
	"path/filepath"
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
