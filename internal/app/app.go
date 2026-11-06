package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"

	"tablume/internal/dbx"
	"tablume/internal/i18n"
	"tablume/internal/store"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// App is the service bound to the frontend; every exported method becomes a
// JS function in frontend/bindings.
type App struct {
	window *application.WebviewWindow // the main window
	// fileRoot is the only folder the browser build reads files from
	// ("" in the desktop app).
	fileRoot string
	store    *store.Store
	db       *dbx.Manager
	tabWins  tabWindows
}

func NewApp() *App {
	return &App{
		db:    dbx.NewManager(),
		store: &store.Store{},
		tabWins: tabWindows{
			wins:    map[string]*application.WebviewWindow{},
			state:   map[string]string{},
			closing: map[string]bool{},
		},
	}
}

func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s, err := store.Open()
	if err != nil {
		application.Get().Logger.Error("open store", "error", err)
		return nil
	}
	a.store = s
	return nil
}

func (a *App) ServiceShutdown() error {
	a.tabWins.mu.Lock()
	a.tabWins.quitting = true
	a.tabWins.mu.Unlock()
	for _, c := range a.store.List() {
		a.db.Disconnect(c.ID)
	}
	return nil
}

// ---------------------------------------------------------------- connections

func (a *App) ListConnections() []dbx.ConnConfig { return a.store.List() }

func (a *App) SaveConnection(c dbx.ConnConfig) (dbx.ConnConfig, error) { return a.store.Save(c) }

// SetSchemaFilter stores which schemas the tree shows for a connection.
func (a *App) SetSchemaFilter(id string, f *dbx.SchemaFilter) error {
	return a.store.SetSchemaFilter(id, f)
}

// ReorderConnections saves the order of the connection list.
func (a *App) ReorderConnections(ids []string) error { return a.store.Reorder(ids) }

func (a *App) DeleteConnection(id string) error {
	a.db.Disconnect(id)
	return a.store.Delete(id)
}

// TestConnection tries the settings from the form. An empty password on an
// existing connection falls back to the stored one.
func (a *App) TestConnection(c dbx.ConnConfig) (TestResult, error) {
	if c.ID != "" {
		if saved, ok := a.store.Get(c.ID); ok {
			if c.Password == "" {
				c.Password = saved.Password
			}
			if c.SSHPassword == "" {
				c.SSHPassword = saved.SSHPassword
			}
		}
	}
	if err := a.checkFiles(&c); err != nil {
		return TestResult{}, err
	}
	v, fp, err := dbx.Test(c)
	return TestResult{Version: v, SSHHostKey: fp}, err
}

type TestResult struct {
	Version    string `json:"version"`
	SSHHostKey string `json:"sshHostKey"`
}

// NeedsPassword reports whether Connect must be given a password.
func (a *App) NeedsPassword(id string) bool {
	c, ok := a.store.Get(id)
	return ok && !c.SavePassword && c.Driver != "sqlite"
}

func (a *App) Connect(id, password string) (string, error) {
	c, ok := a.store.Get(id)
	if !ok {
		return "", i18n.Error("연결 정보를 찾을 수 없습니다")
	}
	if password != "" {
		c.Password = password
	}
	if err := a.checkFiles(&c); err != nil {
		return "", err
	}
	v, err := a.db.Connect(c)
	if err == nil && c.SSH && c.SSHHostKey == "" {
		if fp := a.db.SSHHostKey(id); fp != "" {
			_ = a.store.SetSSHHostKey(id, fp)
		}
	}
	return v, err
}

// SupportedDrivers lists the databases this build includes (see the no_*
// build tags in internal/dbx).
func (a *App) SupportedDrivers() []string { return dbx.SupportedDrivers() }

func (a *App) Disconnect(id string)       { a.db.Disconnect(id) }
func (a *App) IsConnected(id string) bool { return a.db.Connected(id) }

// ---------------------------------------------------------------- metadata

func (a *App) ListSchemas(id string) (dbx.SchemaList, error) { return a.db.Schemas(id) }

func (a *App) ListTables(id, schema string) ([]dbx.Table, error) { return a.db.Tables(id, schema) }
func (a *App) TableStats(id, schema string) ([]dbx.Table, error) { return a.db.TableStats(id, schema) }

func (a *App) ListColumns(id, schema string, t dbx.Table) ([]dbx.Column, error) {
	return a.db.Columns(id, schema, t)
}

func (a *App) CompletionColumns(id, schema string) (map[string][]string, error) {
	return a.db.AllColumns(id, schema)
}

func (a *App) SelectSQL(id, schema string, t dbx.Table, where, orderBy string, limit, offset int) (string, error) {
	return a.db.SelectSQL(id, schema, t, where, orderBy, limit, offset)
}

func (a *App) ListRoutines(id, schema string) ([]dbx.Routine, error) {
	return a.db.Routines(id, schema)
}

func (a *App) RoutineSource(id, schema string, r dbx.Routine) (string, error) {
	return a.db.RoutineSource(id, schema, r)
}

func (a *App) PreviewAlter(req dbx.AlterRequest) ([]string, error) { return a.db.PreviewAlter(req) }

func (a *App) ApplyAlter(req dbx.AlterRequest) (dbx.AlterResult, error) { return a.db.ApplyAlter(req) }

// SchemaGraph returns a schema's tables, columns and foreign keys for the ERD.
func (a *App) SchemaGraph(id, schema string) (dbx.SchemaGraph, error) {
	return a.db.SchemaGraph(id, schema)
}

func (a *App) PreviewCreateTable(req dbx.CreateRequest) ([]string, error) {
	return a.db.PreviewCreateTable(req)
}

func (a *App) CreateTable(req dbx.CreateRequest) (dbx.CreateResult, error) {
	return a.db.CreateTable(req)
}

func (a *App) TableOptions(id, schema string, t dbx.Table) (dbx.TableOptions, error) {
	return a.db.TableOptions(id, schema, t)
}

func (a *App) ListForeignKeys(id, schema string, t dbx.Table) ([]dbx.TableFK, error) {
	return a.db.ListForeignKeys(id, schema, t)
}

func (a *App) PreviewForeignKeys(req dbx.FKRequest) ([]string, error) {
	return a.db.PreviewForeignKeys(req)
}

func (a *App) ApplyForeignKeys(req dbx.FKRequest) (dbx.AlterResult, error) {
	return a.db.ApplyForeignKeys(req)
}

func (a *App) ListIndexes(id, schema string, t dbx.Table) ([]dbx.Index, error) {
	return a.db.ListIndexes(id, schema, t)
}

func (a *App) PreviewIndexes(req dbx.IndexRequest) ([]string, error) { return a.db.PreviewIndexes(req) }

func (a *App) ApplyIndexes(req dbx.IndexRequest) (dbx.AlterResult, error) {
	return a.db.ApplyIndexes(req)
}

// AppendLog adds what a tab ran to its connection's run log.
func (a *App) AppendLog(id string, entries []store.LogEntry) error {
	return a.store.AppendLog(id, entries)
}

// ReadLog returns a connection's run log, newest first, filtered by q.
func (a *App) ReadLog(id, q string, limit int) ([]store.LogEntry, error) {
	return a.store.ReadLog(id, q, limit)
}

// ClearLog deletes a connection's run log.
func (a *App) ClearLog(id string) error { return a.store.ClearLog(id) }

// ObjectListSQL returns a query listing a schema's tables or routines ("tables", "routines").
func (a *App) ObjectListSQL(id, schema, kind string) (string, error) {
	return a.db.ObjectListSQL(id, schema, kind)
}

// CreateSchema makes a schema (MySQL, SQL Server: a database); returns the statement it ran.
func (a *App) CreateSchema(id, name string) (string, error) { return a.db.CreateSchema(id, name) }

// DropSchema drops a schema and what is in it; returns the statement it ran.
func (a *App) DropSchema(id, name string) (string, error) { return a.db.DropSchema(id, name) }

// DropTable drops a table or view; returns the statement it ran.
func (a *App) DropTable(id, schema string, t dbx.Table) (string, error) {
	return a.db.DropTable(id, schema, t)
}

// TruncateTable deletes every row of a table; returns the statement it ran.
func (a *App) TruncateTable(id, schema string, t dbx.Table) (string, error) {
	return a.db.TruncateTable(id, schema, t)
}

// RenameTable renames a table or view; returns the statement it ran.
func (a *App) RenameTable(id, schema string, t dbx.Table, name string) (string, error) {
	return a.db.RenameTable(id, schema, t, name)
}

// FetchPage reads the next rows of a paged result (see StmtResult.Paged).
func (a *App) FetchPage(tabID, connID, sql string, offset, n int) (dbx.FetchResponse, error) {
	return a.db.FetchPage(tabID, connID, sql, offset, n)
}

func (a *App) CountResult(tabID, connID, sql string) (int64, error) {
	return a.db.CountResult(tabID, connID, sql)
}

func (a *App) CountRows(id, schema string, t dbx.Table, where string) (int64, error) {
	return a.db.CountRows(id, schema, t, where)
}

func (a *App) QuoteIdent(id, name string) (string, error) { return a.db.QuoteIdent(id, name) }

func (a *App) TableDDL(id, schema string, t dbx.Table) (string, error) {
	return a.db.DDL(id, schema, t)
}

// ---------------------------------------------------------------- editing

func (a *App) EditInfo(id, schema string, source, columns []string) (dbx.EditInfo, error) {
	return a.db.EditInfo(id, schema, source, columns)
}

func (a *App) PreviewChanges(req dbx.ApplyRequest) ([]string, error) { return a.db.PreviewChanges(req) }

func (a *App) ApplyChanges(req dbx.ApplyRequest) (int, error) { return a.db.ApplyChanges(req) }

func (a *App) QualifiedName(id, schema string, t dbx.Table) (string, error) {
	return a.db.QualifiedName(id, schema, t)
}

// ---------------------------------------------------------------- queries

func (a *App) Execute(req dbx.ExecRequest) (dbx.ExecResponse, error) { return a.db.Execute(req) }

func (a *App) FetchMore(tabID, cursorID string, n int) (dbx.FetchResponse, error) {
	return a.db.FetchMore(tabID, cursorID, n)
}

func (a *App) Cancel(tabID string)       { a.db.Cancel(tabID) }
func (a *App) CloseSession(tabID string) { a.db.CloseSession(tabID) }

// ---------------------------------------------------------------- files

func (a *App) openFile(ctx context.Context, o application.OpenFileDialogOptions) (string, error) {
	o.CanChooseFiles = true
	o.Window = a.windowOf(ctx)
	return application.Get().Dialog.OpenFileWithOptions(&o).PromptForSingleSelection()
}

func (a *App) PickSQLiteFile(ctx context.Context) (string, error) {
	return a.openFile(ctx, application.OpenFileDialogOptions{
		Title: i18n.T("SQLite 데이터베이스 파일"),
		Filters: []application.FileFilter{
			{DisplayName: "SQLite (*.db;*.sqlite;*.sqlite3;*.db3)", Pattern: "*.db;*.sqlite;*.sqlite3;*.db3"},
			{DisplayName: i18n.T("모든 파일"), Pattern: "*"},
		},
	})
}

// PickKeyFile picks an SSH private key, starting in ~/.ssh.
func (a *App) PickKeyFile(ctx context.Context) (string, error) {
	dir := ""
	if home, err := os.UserHomeDir(); err == nil {
		if st, err := os.Stat(filepath.Join(home, ".ssh")); err == nil && st.IsDir() {
			dir = filepath.Join(home, ".ssh")
		}
	}
	// Key files are often hidden-ish (no extension, in a dot directory).
	return a.openFile(ctx, application.OpenFileDialogOptions{Title: i18n.T("SSH 개인 키 파일"), Directory: dir, ShowHiddenFiles: true})
}

// SaveTextFile asks for a path and writes content. Returns "" if cancelled.
func (a *App) SaveTextFile(ctx context.Context, defaultName, content string) (string, error) {
	path, err := application.Get().Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Filename:             defaultName,
		CanCreateDirectories: true,
		Window:               a.windowOf(ctx),
	}).PromptForSingleSelection()
	if err != nil || path == "" {
		return "", err
	}
	return path, os.WriteFile(path, []byte(content), 0o644)
}

type OpenedFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func (a *App) OpenSQLFile(ctx context.Context) (*OpenedFile, error) {
	path, err := a.openFile(ctx, application.OpenFileDialogOptions{
		Filters: []application.FileFilter{{DisplayName: "SQL (*.sql)", Pattern: "*.sql"}, {DisplayName: i18n.T("모든 파일"), Pattern: "*"}},
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

// Workspace (open tabs and their SQL) lives next to the connection list,
// since webview localStorage isn't reliably persistent on every platform.
func workspacePath() string {
	dir, _ := store.Dir()
	return filepath.Join(dir, "workspace.json")
}

func (a *App) LoadWorkspace() string {
	b, _ := os.ReadFile(workspacePath())
	return string(b)
}

func (a *App) SaveWorkspace(data string) error {
	return os.WriteFile(workspacePath(), []byte(data), 0o600)
}

// loadLanguage takes the interface language saved with the workspace, so
// messages before the page says (the macOS menu, the server log) use it.
func loadLanguage() {
	var w struct {
		Lang string `json:"lang"`
	}
	if b, err := os.ReadFile(workspacePath()); err == nil && json.Unmarshal(b, &w) == nil && w.Lang != "" {
		i18n.Set(w.Lang)
	}
}

// Version is the app's release version (the VERSION file).
func (a *App) Version() string { return version }

// SetLanguage is the interface language chosen in the app ("ko" or "en"):
// the backend's messages follow it.
func (a *App) SetLanguage(lang string) {
	if i18n.Set(lang) && goruntime.GOOS == "darwin" {
		if app := application.Get(); app != nil {
			app.Menu.Set(macMenu())
		}
	}
}

// SetDarkTitleBar matches the calling window's native title bar (Windows)
// to the app theme.
func (a *App) SetDarkTitleBar(ctx context.Context, dark bool) {
	if w := a.windowOf(ctx); w != nil {
		setDarkTitleBar(w, dark)
	}
}
