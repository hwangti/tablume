package main

import (
	_ "embed"
	"strings"
)

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

func appVersion() string {
	if version != "" {
		return version
	}
	return strings.TrimSpace(versionFile)
}
