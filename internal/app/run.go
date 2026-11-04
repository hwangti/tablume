package app

import (
	"io/fs"
	"path/filepath"
	goruntime "runtime"

	"tablume/internal/store"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// version is the release version, set by Run.
var version string

// Run builds the app around what the main package embeds (the built
// frontend, the icon for the macOS about panel, the release version) and
// runs it until it quits.
func Run(assets fs.FS, icon []byte, ver string) error {
	version = ver
	svc := NewApp()

	// Keep WebView2's cache next to our own config (%APPDATA%\Tablume\webview)
	// instead of the default next to the executable's name.
	webviewDir := ""
	if dir, err := store.Dir(); err == nil {
		webviewDir = filepath.Join(dir, "webview")
	}

	opts := application.Options{
		Name:        "Tablume",
		Description: "A lightweight database client",
		Icon:        icon,
		Services:    []application.Service{application.NewService(svc)},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Windows: application.WindowsOptions{WebviewUserDataPath: webviewDir},
	}
	start := setupPlatform(svc, &opts)
	app := application.New(opts)

	if goruntime.GOOS == "darwin" {
		app.Menu.Set(macMenu())
	}

	mainOpts := application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Tablume",
		Width:            1360,
		Height:           860,
		MinWidth:         800,
		MinHeight:        500,
		BackgroundColour: application.NewRGB(30, 31, 34),
		URL:              "/",
	}
	if platform == "desktop" {
		loadWindowState(&mainOpts)
	}
	svc.window = app.Window.NewWithOptions(mainOpts)
	if platform == "desktop" {
		keepWindowState(svc.window)
	}
	svc.quitWithMain(svc.window)
	start()

	return app.Run()
}
