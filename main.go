// Tablume: a lightweight database client. The app lives in internal/app;
// this package only embeds the files that sit at the repository root.
package main

import (
	"embed"
	"log"
	"strings"

	"tablume/internal/app"
)

//go:embed all:frontend/dist
var assets embed.FS

// Shown in the macOS "Tablume에 관하여" panel.
//
//go:embed build/appicon.png
var appIcon []byte

// The release version lives in the VERSION file; every build embeds it, so
// a plain `go build` knows it too. `wails3 task version VERSION=x.y.z`
// changes it together with the copies the OS reads (Info.plist, the .exe's
// version info, package.json).
//
//go:embed VERSION
var versionFile string

// version overrides VERSION when set at build time, e.g. for a CI build:
// go build -ldflags "-X main.version=0.9.1-rc1"
var version string

func main() {
	if version == "" {
		version = strings.TrimSpace(versionFile)
	}
	if err := app.Run(assets, appIcon, version); err != nil {
		log.Fatal(err)
	}
}
