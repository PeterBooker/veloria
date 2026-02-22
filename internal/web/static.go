package web

import (
	"io/fs"
	"net/http"
)

// StaticImage serves a pre-loaded embedded image with long cache headers.
func StaticImage(fsys fs.FS, filename string) http.HandlerFunc {
	data, err := fs.ReadFile(fsys, filename)
	if err != nil {
		panic("missing embedded asset: " + filename)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
		_, _ = w.Write(data)
	}
}
