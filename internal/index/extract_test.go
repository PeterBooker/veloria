package index

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testEntrySize = 1024

// writeZip writes an archive of `entries` files of testEntrySize zero bytes
// each under a common "pkg/" directory and returns its path.
func writeZip(t *testing.T, entries int) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	payload := make([]byte, testEntrySize)
	for i := range entries {
		w, err := zw.Create(fmt.Sprintf("pkg/f%03d.txt", i))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return writeArchive(t, buf.Bytes())
}

// writeSharedDataZip writes an archive that stores one deflated member of
// testEntrySize zero bytes and lists it under `entries` different names, so
// the extracted size grows with the entry count while the archive stays tiny.
func writeSharedDataZip(t *testing.T, entries int) string {
	t.Helper()
	payload := make([]byte, testEntrySize)
	var deflated bytes.Buffer
	fw, err := flate.NewWriter(&deflated, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := fw.Close(); err != nil {
		t.Fatal(err)
	}
	crc := crc32.ChecksumIEEE(payload)
	compSize, rawSize := uint32(deflated.Len()), uint32(len(payload))

	var buf bytes.Buffer
	put := func(fields ...any) {
		for _, f := range fields {
			if err := binary.Write(&buf, binary.LittleEndian, f); err != nil {
				t.Fatal(err)
			}
		}
	}
	name := func(i int) string { return fmt.Sprintf("pkg/f%03d.txt", i) }

	// The only local file header and its data.
	put(uint32(0x04034b50), uint16(20), uint16(0), uint16(zip.Deflate), uint16(0), uint16(0),
		crc, compSize, rawSize, uint16(len(name(0))), uint16(0))
	buf.WriteString(name(0))
	buf.Write(deflated.Bytes())

	// Central directory: every entry points at offset 0.
	dirStart := buf.Len()
	for i := range entries {
		put(uint32(0x02014b50), uint16(20), uint16(20), uint16(0), uint16(zip.Deflate), uint16(0), uint16(0),
			crc, compSize, rawSize, uint16(len(name(i))),
			uint16(0), uint16(0), uint16(0), uint16(0), uint32(0), uint32(0))
		buf.WriteString(name(i))
	}
	dirSize := buf.Len() - dirStart
	put(uint32(0x06054b50), uint16(0), uint16(0), uint16(entries), uint16(entries),
		uint32(dirSize), uint32(dirStart), uint16(0))

	return writeArchive(t, buf.Bytes())
}

func writeArchive(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// assertNoOutput fails if anything was left at dest.
func assertNoOutput(t *testing.T, dest string) {
	t.Helper()
	var left []string
	_ = filepath.WalkDir(dest, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			left = append(left, path)
		}
		return nil
	})
	if len(left) > 0 {
		t.Errorf("%d extracted files left behind after a failed extraction, e.g. %s", len(left), left[0])
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("destination %s still exists after a failed extraction (stat err=%v)", dest, err)
	}
}

func TestUnzip_TotalSizeLimit(t *testing.T) {
	limits := extractLimits{fileSize: 4 * testEntrySize, totalSize: 4 * testEntrySize, entries: 100}
	tests := []struct {
		name string
		src  string
	}{
		{"distinct entries", writeZip(t, 8)},
		{"entries sharing one stored member", writeSharedDataZip(t, 8)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "out")
			if err := os.MkdirAll(dest, 0o750); err != nil {
				t.Fatal(err)
			}

			stats, err := unzipWithFilterAndStats(tt.src, dest, nil, 100, limits)
			if err == nil {
				t.Fatalf("extracted %d bytes in %d files past a total limit of %d bytes", stats.TotalSize, stats.FileCount, limits.totalSize)
			}
			if !strings.Contains(err.Error(), "maximum total extracted size of 4096 bytes") {
				t.Errorf("error should state the total limit, got: %v", err)
			}
			assertNoOutput(t, dest)
		})
	}
}

func TestUnzip_EntryLimit(t *testing.T) {
	limits := extractLimits{fileSize: 4 * testEntrySize, totalSize: 1 << 20, entries: 5}
	dest := filepath.Join(t.TempDir(), "out")

	stats, err := unzipWithFilterAndStats(writeZip(t, 6), dest, nil, 100, limits)
	if err == nil {
		t.Fatalf("extracted %d files past an entry limit of %d", stats.FileCount, limits.entries)
	}
	if !strings.Contains(err.Error(), "6 entries") || !strings.Contains(err.Error(), "maximum of 5") {
		t.Errorf("error should state the entry count and limit, got: %v", err)
	}
	assertNoOutput(t, dest)
}

func TestUnzip_FileSizeLimit(t *testing.T) {
	limits := extractLimits{fileSize: testEntrySize - 1, totalSize: 1 << 20, entries: 100}
	dest := filepath.Join(t.TempDir(), "out")

	_, err := unzipWithFilterAndStats(writeZip(t, 2), dest, nil, 100, limits)
	if err == nil || !strings.Contains(err.Error(), "file exceeds maximum decompressed size of 1023 bytes") {
		t.Fatalf("expected the per-file limit error, got: %v", err)
	}
	assertNoOutput(t, dest)
}

func TestUnzip_AtLimitsSucceeds(t *testing.T) {
	// Exactly at every limit: 4 entries of 1024 bytes each.
	limits := extractLimits{fileSize: testEntrySize, totalSize: 4 * testEntrySize, entries: 4}
	dest := filepath.Join(t.TempDir(), "out")

	stats, err := unzipWithFilterAndStats(writeZip(t, 4), dest, nil, 100, limits)
	if err != nil {
		t.Fatalf("an archive exactly at the limits must extract, got: %v", err)
	}
	if stats.FileCount != 4 || stats.TotalSize != 4*testEntrySize {
		t.Errorf("stats = %d files, %d bytes; want 4 files, %d bytes", stats.FileCount, stats.TotalSize, 4*testEntrySize)
	}
	for i := range 4 {
		info, err := os.Stat(filepath.Join(dest, fmt.Sprintf("f%03d.txt", i)))
		if err != nil || info.Size() != testEntrySize {
			t.Errorf("file %d: err=%v", i, err)
		}
	}
}

func TestUnzipWithStats_DefaultLimits(t *testing.T) {
	want := extractLimits{fileSize: 256 << 20, totalSize: 4 << 30, entries: 250_000}
	if defaultExtractLimits != want {
		t.Errorf("defaultExtractLimits = %+v, want %+v", defaultExtractLimits, want)
	}

	dest := filepath.Join(t.TempDir(), "out")
	stats, err := UnzipWithStats(writeZip(t, 3), dest)
	if err != nil {
		t.Fatalf("UnzipWithStats: %v", err)
	}
	if stats.FileCount != 3 || stats.TotalSize != 3*testEntrySize {
		t.Errorf("stats = %d files, %d bytes; want 3 files, %d bytes", stats.FileCount, stats.TotalSize, 3*testEntrySize)
	}
}
