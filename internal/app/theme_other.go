//go:build !windows

package app

import "github.com/wailsapp/wails/v3/pkg/application"

// The title bar follows the window appearance elsewhere.
func setDarkTitleBar(*application.WebviewWindow, bool) {}
