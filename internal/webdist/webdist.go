// Package webdist embeds the built frontend for same-origin production
// serving. Build flow: `cd web && npm run build` then copy web/dist/*
// here (see scripts/build.sh). The committed placeholder keeps
// development builds compileable.
package webdist

import "embed"

//go:embed all:dist
var Dist embed.FS
