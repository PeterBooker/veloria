package web

import (
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
)

// StaticImage serves a pre-loaded embedded asset with long cache headers.
// Content-Type is detected from the filename extension.
func StaticImage(fsys fs.FS, filename string) http.HandlerFunc {
	data, err := fs.ReadFile(fsys, filename)
	if err != nil {
		panic("missing embedded asset: " + filename)
	}

	contentType := mime.TypeByExtension(filepath.Ext(filename))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
		_, _ = w.Write(data)
	}
}
