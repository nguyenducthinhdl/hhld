package view

import "embed"

// FS is the admin dashboard HTML/CSS/JS served at GET /view/.
//
//go:embed *.html *.css *.js
var FS embed.FS
