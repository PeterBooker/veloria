package assets

import "embed"

//go:generate sh -c "npm --prefix ../frontend install --no-audit --no-fund && npm --prefix ../frontend run build"

//go:embed og-base.png og-default.png fonts/*.ttf static/css static/js static/fonts static/favicon.ico static/favicon.svg
var FS embed.FS
