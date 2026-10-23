// Package web holds the single-page front-end that is compiled into the
// binary. Keeping it embedded means the shipped app is one self-contained
// file with no external asset directory to keep in sync.
package web

import "embed"

//go:embed index.html assets
var FS embed.FS
