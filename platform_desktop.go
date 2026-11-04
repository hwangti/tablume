//go:build !server

package main

import "github.com/wailsapp/wails/v3/pkg/application"

const platform = "desktop"

// setupPlatform adjusts the app options for this build and returns what to
// start once the app exists. Nothing to do for the desktop app.
func setupPlatform(*App, *application.Options) func() {
	loadLanguage()
	return func() {}
}
