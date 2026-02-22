package assets

import "embed"

//go:embed og-base.png og-default.png fonts/*.ttf
var FS embed.FS
