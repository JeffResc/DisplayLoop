// Package assets embeds all static files and HTML templates into the binary.
package assets

import "embed"

//go:embed templates/*.html
var Templates embed.FS

//go:embed static/black.png
var Static embed.FS
