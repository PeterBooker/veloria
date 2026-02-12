package index

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"veloria/internal/codesearch/index"
)

// flushQuiet calls ix.Flush() with the standard logger silenced,
// suppressing the codesearch library's unconditional log output.
func flushQuiet(ix *index.IndexWriter) {
	prev := log.Writer()
	log.SetOutput(io.Discard)
	ix.Flush()
	log.SetOutput(prev)
}

func New(slug string) *index.IndexWriter {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("failed to get working directory: %v", err)
	}

	ix := index.Create(filepath.Join(wd, "testdata", "indexes", slug, "trigrams"))
	ix.Verbose = true
	ix.Zip = true

	var roots []index.Path
	roots = append(roots, index.MakePath(filepath.Join(wd, "testdata", "extract", slug)))
	ix.AddRoots(roots)

	for _, root := range roots {
		if err := filepath.Walk(root.String(), func(path string, info os.FileInfo, err error) error {
			if _, elem := filepath.Split(path); elem != "" {
				// Skip various temporary or "hidden" files or directories.
				if elem[0] == '.' || elem[0] == '#' || elem[0] == '~' || elem[len(elem)-1] == '~' {
					if info.IsDir() {
						return filepath.SkipDir
					}

					return nil
				}
			}

			if err != nil {
				log.Printf("%s: %s", path, err)

				return nil
			}

			if info != nil && info.Mode()&os.ModeType == 0 {
				if err := ix.AddFile(path); err != nil {
					return nil
				}
			}
			return nil
		}); err != nil {
			log.Printf("failed to walk %s: %v", root, err)
		}
	}

	flushQuiet(ix)

	return ix
}

func IndexDir(indexDir string, indexSrc string, slug string) *index.IndexWriter {
	trigramsPath := filepath.Join(indexDir, slug, "trigrams")
	return IndexDirToFile(indexSrc, trigramsPath)
}

// IndexDirToFile creates a trigram index from indexSrc and writes it to trigramsPath.
func IndexDirToFile(indexSrc string, trigramsPath string) *index.IndexWriter {
	// Ensure parent directory exists and "touch" the trigrams file.
	if err := os.MkdirAll(filepath.Dir(trigramsPath), 0o750); err != nil {
		log.Fatalf("failed to create index directory %q: %v", filepath.Dir(trigramsPath), err)
	}
	if _, err := os.Stat(trigramsPath); os.IsNotExist(err) {
		f, err := os.OpenFile(trigramsPath, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- path built from internal config
		if err != nil {
			log.Fatalf("failed to create trigrams file %q: %v", trigramsPath, err)
		}
		_ = f.Close()
	} else if err != nil {
		log.Fatalf("failed to stat trigrams file %q: %v", trigramsPath, err)
	}

	ix := index.Create(trigramsPath)
	ix.Verbose = false
	ix.Zip = false

	var roots []index.Path
	roots = append(roots, index.MakePath(filepath.Join(indexSrc)))
	ix.AddRoots(roots)

	for _, root := range roots {
		if err := filepath.Walk(root.String(), func(path string, info os.FileInfo, err error) error {
			if _, elem := filepath.Split(path); elem != "" {
				// Skip temporary or hidden files/dirs.
				if elem[0] == '.' || elem[0] == '#' || elem[0] == '~' || elem[len(elem)-1] == '~' {
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}

			if err != nil {
				log.Printf("%s: %s", path, err)
				return nil
			}

			if info != nil && info.Mode()&os.ModeType == 0 {
				if err := ix.AddFile(path); err != nil {
					return nil
				}
			}
			return nil
		}); err != nil {
			log.Printf("failed to walk %s: %v", root, err)
		}
	}

	flushQuiet(ix)
	return ix
}
