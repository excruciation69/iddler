package ui

import "embed"

// FS holds the web interface, the shared RU/EN strings and the macOS menu bar script.
//
//go:embed *.html *.css *.js *.json *.png *.ico *.svg *.icns js fonts
var FS embed.FS
