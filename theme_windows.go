package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

func setDarkTitleBar(w *application.WebviewWindow, dark bool) {
	application.InvokeSync(func() {
		if hwnd := w.NativeWindow(); hwnd != nil && w32.SupportsThemes() {
			w32.SetTheme(uintptr(hwnd), dark)
		}
	})
}
