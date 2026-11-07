//go:build !server

package app

import "github.com/wailsapp/wails/v3/pkg/application"

const platform = "desktop"

// setupPlatform adjusts the app options for this build and returns what to
// start once the app exists. Nothing to do for the desktop app.
func setupPlatform(*App, *application.Options) func() {
	loadLanguage()
	return func() {}
}

// openURL opens a page in the OS's browser.
func openURL(url string) error { return application.Get().Browser.OpenURL(url) }
