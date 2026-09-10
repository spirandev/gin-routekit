package main

import "embed"

// The Docusaurus build output. docusaurus.config.js sets outDir to
// "go/portal-build", so `npm run build` regenerates this directory in place.
// The committed index.html is a placeholder that keeps `go build` working
// without Node.
//
//go:embed all:portal-build
var portalAssets embed.FS
