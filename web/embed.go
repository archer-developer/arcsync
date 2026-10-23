// Package web embeds the frontend static assets.
package web

import "embed"

//go:embed index.html css js img
var FS embed.FS
