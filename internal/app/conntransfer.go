package app

import (
	"context"
	"os"
	"path/filepath"

	"tablume/internal/dbx"
	"tablume/internal/i18n"
	"tablume/internal/store"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ExportConnections is the JSON file of these connections (all when none),
// without passwords.
func (a *App) ExportConnections(ids []string) (string, error) {
	b, err := a.store.Export(ids)
	return string(b), err
}

// ImportConnections adds the connections of an exported file.
func (a *App) ImportConnections(content string) (store.ImportResult, error) {
	return a.store.Import([]byte(content), dbx.SupportedDrivers())
}

// OpenJSONFile asks for a .json file and reads it; nil if cancelled.
func (a *App) OpenJSONFile(ctx context.Context) (*OpenedFile, error) {
	path, err := a.openFile(ctx, application.OpenFileDialogOptions{
		Filters: []application.FileFilter{{DisplayName: "JSON (*.json)", Pattern: "*.json"}, {DisplayName: i18n.T("모든 파일"), Pattern: "*"}},
	})
	if err != nil || path == "" {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return &OpenedFile{Name: filepath.Base(path), Content: string(b)}, nil
}
