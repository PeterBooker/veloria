package templates

import "embed"

//go:embed layouts/*.html pages/*.html partials/*.html
var FS embed.FS
