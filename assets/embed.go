package assets

import "embed"

//go:embed og-base.png og-default.png fonts/*.ttf static/css static/js static/fonts
var FS embed.FS
