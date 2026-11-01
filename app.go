package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"tablume/internal/dbx"
	"tablume/internal/store"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is bound to the frontend; every exported method becomes a JS function.
type App struct {
	ctx   context.Context
	store *store.Store
	db    *dbx.Manager
}

func NewApp() *App {
	return &App{db: dbx.NewManager()}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	s, err := store.Open()
	if err != nil {
		runtime.LogErrorf(ctx, "open store: %v", err)
		s = &store.Store{}
	}
	a.store = s
}

func (a *App) shutdown(context.Context) {
	for _, c := range a.store.List() {
		a.db.Disconnect(c.ID)
	}
}

// ---------------------------------------------------------------- connections

func (a *App) ListConnections() []dbx.ConnConfig { return a.store.List() }

func (a *App) SaveConnection(c dbx.ConnConfig) (dbx.ConnConfig, error) { return a.store.Save(c) }

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
		return "", errors.New("연결 정보를 찾을 수 없습니다")
	}
	if password != "" {
		c.Password = password
	}
	v, err := a.db.Connect(c)
	if err == nil && c.SSH && c.SSHHostKey == "" {
		if fp := a.db.SSHHostKey(id); fp != "" {
			_ = a.store.SetSSHHostKey(id, fp)
		}
	}
	return v, err
}

func (a *App) Disconnect(id string)       { a.db.Disconnect(id) }
func (a *App) IsConnected(id string) bool { return a.db.Connected(id) }

// ---------------------------------------------------------------- metadata

func (a *App) ListSchemas(id string) (dbx.SchemaList, error) { return a.db.Schemas(id) }

func (a *App) ListTables(id, schema string) ([]dbx.Table, error) { return a.db.Tables(id, schema) }

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

func (a *App) PickSQLiteFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "SQLite 데이터베이스 파일",
		Filters: []runtime.FileFilter{
			{DisplayName: "SQLite (*.db;*.sqlite;*.sqlite3;*.db3)", Pattern: "*.db;*.sqlite;*.sqlite3;*.db3"},
			{DisplayName: "모든 파일", Pattern: "*"},
		},
	})
}

// PickKeyFile picks an SSH private key, starting in ~/.ssh.
func (a *App) PickKeyFile() (string, error) {
	dir := ""
	if home, err := os.UserHomeDir(); err == nil {
		if st, err := os.Stat(filepath.Join(home, ".ssh")); err == nil && st.IsDir() {
			dir = filepath.Join(home, ".ssh")
		}
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "SSH 개인 키 파일", DefaultDirectory: dir})
}

// SaveTextFile asks for a path and writes content. Returns "" if cancelled.
func (a *App) SaveTextFile(defaultName, content string) (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{DefaultFilename: defaultName})
	if err != nil || path == "" {
		return "", err
	}
	return path, os.WriteFile(path, []byte(content), 0o644)
}

type OpenedFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func (a *App) OpenSQLFile() (*OpenedFile, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Filters: []runtime.FileFilter{{DisplayName: "SQL (*.sql)", Pattern: "*.sql"}, {DisplayName: "모든 파일", Pattern: "*"}},
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
