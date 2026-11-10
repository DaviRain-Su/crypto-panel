package app

import _ "embed"

// The app's mark, the same art resources/icon.png gives the bundled .app.
// A binary run outside a bundle — `go run .` in development — has no
// Info.plist to take its Dock icon from, so Ready hands the Dock this
// image. Regenerate both copies with `go run ./tools/icon`.
//
//go:embed logo.png
var appLogoPNG []byte
