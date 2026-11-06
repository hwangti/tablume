package app

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"tablume/internal/dbx"
	"tablume/internal/i18n"
)

// ---------------------------------------------------------------- export
// Whole tables go straight from the database to the file: in the desktop
// build to a file picked here, in the browser build as a download
// (serveExport) instead of passing through a binding call.

type ExportResult struct {
	Path string `json:"path"` // "" if cancelled
	Rows int64  `json:"rows"`
}

func (a *App) ExportData(ctx context.Context, req dbx.ExportRequest) (ExportResult, error) {
	path, err := application.Get().Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Filename:             dbx.ExportName(req),
		CanCreateDirectories: true,
		Window:               a.windowOf(ctx),
	}).PromptForSingleSelection()
	if err != nil || path == "" {
		return ExportResult{}, err
	}
	f, err := os.Create(path)
	if err != nil {
		return ExportResult{}, err
	}
	n, err := a.db.Export(context.Background(), f, req)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path) // not a file that looks complete
		return ExportResult{}, err
	}
	return ExportResult{Path: path, Rows: n}, nil
}

// serveExport answers GET /x/export?req={ExportRequest as JSON} with the
// file (browser build).
func (a *App) serveExport(w http.ResponseWriter, r *http.Request) {
	var req dbx.ExportRequest
	if err := json.Unmarshal([]byte(r.URL.Query().Get("req")), &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	lw := &downloadWriter{w: w, name: dbx.ExportName(req)}
	if _, err := a.db.Export(r.Context(), lw, req); err != nil && !lw.started {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// downloadWriter sends the download headers with the first bytes, so an
// error before any (no such table) can still be an error response.
type downloadWriter struct {
	w       http.ResponseWriter
	name    string
	started bool
}

func (d *downloadWriter) Write(b []byte) (int, error) {
	if !d.started {
		d.started = true
		h := d.w.Header()
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": d.name}))
		h.Set("Cache-Control", "no-store")
	}
	return d.w.Write(b)
}

// ---------------------------------------------------------------- import

type DataFile struct {
	Name string `json:"name"`
	Data []byte `json:"data"` // as read; the frontend decodes it (UTF-8, else CP949)
}

// OpenDataFile picks a CSV file to import (desktop build).
func (a *App) OpenDataFile(ctx context.Context) (*DataFile, error) {
	path, err := a.openFile(ctx, application.OpenFileDialogOptions{
		Filters: []application.FileFilter{
			{DisplayName: "CSV (*.csv;*.tsv;*.txt)", Pattern: "*.csv;*.tsv;*.txt"},
			{DisplayName: i18n.T("모든 파일"), Pattern: "*"},
		},
	})
	if err != nil || path == "" {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return &DataFile{Name: filepath.Base(path), Data: b}, nil
}
