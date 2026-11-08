// Package web embeds the interface and its two fonts. Everything is local: no CDN, no remote
// font, no build step, one binary. The interface is a document with a price rail per item,
// not a marketing page — the list is the product.
package web

import "embed"

//go:embed index.html styles.css app.js
//go:embed fonts/*.woff2
var FS embed.FS
