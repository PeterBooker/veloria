package index

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"unicode/utf8"

	cindex "veloria/internal/codesearch/index"
)

// Open opens an existing trigram index at the given directory path.
// The path should point to the directory containing the "trigrams" file.
// Returns nil if the trigrams file is missing or the index is corrupt, so
// callers can delete the bad directory and fall back to a sibling version.
func Open(path string) (idx *Index) {
	trigramsPath := filepath.Join(path, "trigrams")

	// Check existence before calling cindex.Open, which log.Fatals on missing files.
	if _, err := os.Stat(trigramsPath); err != nil {
		return nil
	}

	// cindex.Open panics with "corrupt index" on malformed data rather than
	// returning an error. Recover so one bad index cannot crash the service
	// at startup via the goroutines spawned in manager.NewManager.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("corrupt index at %s: %v", trigramsPath, r)
			idx = nil
		}
	}()

	return &Index{
		dir: path,
		idx: cindex.Open(trigramsPath),
	}
}

// isTextFile determines whether the file at the given path is a UTF-8 encoded text file.
func isTextFile(filename string) (ok bool, err error) {
	if filename == "" {
		return false, errors.New("filename must not be empty")
	}

	f, err := os.Open(filename) // #nosec G304 -- filename from internal index walk
	if err != nil {
		return false, err
	}
	defer func() {
		if cerr := f.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	var buf [filePeekSize]byte
	n, err := io.ReadFull(f, buf[:])
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return false, err
	}

	data := buf[:n]

	if n < filePeekSize {
		return utf8.Valid(data), nil
	}

	// read a prefix, allow trailing partial runes
	return validUTF8(data), nil
}

// validUTF8 returns true if p is valid UTF-8,
// allowing for an incomplete multi-byte rune at the end.
// This is useful when processing partial chunks of a larger buffer, such as
// in streaming data or when reading input incrementally.
func validUTF8(p []byte) bool {
	i := 0
	for i < len(p) {
		if p[i] < utf8.RuneSelf {
			i++
			continue
		}
		r, size := utf8.DecodeRune(p[i:])
		if r == utf8.RuneError && size == 1 {
			remaining := len(p) - i
			if remaining < utf8.UTFMax && utf8.RuneStart(p[i]) && !utf8.FullRune(p[i:]) {
				return true // Assume valid: trailing partial rune
			}
			return false
		}
		i += size
	}
	return true
}
