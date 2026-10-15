// Package web embeds the self-contained Live Routes page.
package web

import _ "embed"

// IndexHTML is served at the plugin resource page path.
//
//go:embed index.html
var IndexHTML []byte
