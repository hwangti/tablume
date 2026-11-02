package main

import (
	"embed"
	"log"
	"path/filepath"
	goruntime "runtime"

	"tablume/internal/store"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

// Shown in the macOS "Tablume에 관하여" panel.
//
//go:embed build/appicon.png
var appIcon []byte

func main() {
	svc := NewApp()

	// Keep WebView2's cache next to our own config (%APPDATA%\Tablume\webview)
	// instead of the default next to the executable's name.
	webviewDir := ""
	if dir, err := store.Dir(); err == nil {
		webviewDir = filepath.Join(dir, "webview")
	}

	app := application.New(application.Options{
		Name:        "Tablume",
		Description: "가벼운 데이터베이스 클라이언트",
		Icon:        appIcon,
		Services:    []application.Service{application.NewService(svc)},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Windows: application.WindowsOptions{WebviewUserDataPath: webviewDir},
	})

	if goruntime.GOOS == "darwin" {
		app.Menu.Set(macMenu())
	}

	svc.window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Tablume",
		Width:            1360,
		Height:           860,
		MinWidth:         800,
		MinHeight:        500,
		BackgroundColour: application.NewRGB(30, 31, 34),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
