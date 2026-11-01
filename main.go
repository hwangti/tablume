package main

import (
	"context"
	"embed"
	"path/filepath"
	goruntime "runtime"

	"tablume/internal/store"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

// Shown in the macOS "Tablume에 관하여" panel (Wails' default otherwise).
//
//go:embed build/appicon.png
var appIcon []byte

func main() {
	app := NewApp()

	var appMenu *menu.Menu
	if goruntime.GOOS == "darwin" {
		// Without an Edit menu, Cmd+C / Cmd+V don't reach the webview on macOS.
		appMenu = menu.NewMenu()
		appMenu.Append(menu.AppMenu())
		appMenu.Append(menu.EditMenu())
		appMenu.Append(menu.WindowMenu())
	}

	// Keep WebView2's cache next to our own config (%APPDATA%\Tablume\webview)
	// instead of Wails' default %APPDATA%\Tablume.exe.
	webviewDir := ""
	if dir, err := store.Dir(); err == nil {
		webviewDir = filepath.Join(dir, "webview")
	}

	err := wails.Run(&options.App{
		Title:            "Tablume",
		Width:            1360,
		Height:           860,
		MinWidth:         800,
		MinHeight:        500,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 30, G: 31, B: 34, A: 1},
		Menu:             appMenu,
		OnStartup:        app.startup,
		// The macOS menu is built by then; give it Korean titles.
		OnDomReady: func(context.Context) { localizeMenu() },
		OnShutdown: app.shutdown,
		Bind:       []interface{}{app},
		Mac: &mac.Options{
			About: &mac.AboutInfo{Title: "Tablume", Message: "가벼운 데이터베이스 클라이언트", Icon: appIcon},
		},
		Windows: &windows.Options{
			Theme:               windows.SystemDefault,
			WebviewUserDataPath: webviewDir,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
