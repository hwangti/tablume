package main

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"tablume/internal/dbx"
	"tablume/internal/i18n"
)

// Platform tells the frontend which build it runs in: "desktop" (a native
// window) or "web" (the browser build, -tags server).
func (a *App) Platform() string { return platform }

// TouchSessions keeps the DB sessions of a browser page's tabs open (the
// page calls it every few minutes; sessions nobody touches are closed).
func (a *App) TouchSessions(tabIDs []string) { a.db.TouchSessions(tabIDs) }

// ---------------------------------------------------------------- server files
// In the browser build the server's files stand in for file dialogs: SQLite
// databases and SSH keys are picked from one folder (-files) and nothing
// outside it can be used.

type FileEntry struct {
	Name     string    `json:"name"`
	Dir      bool      `json:"dir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

type FileList struct {
	Root    string      `json:"root"`
	Dir     string      `json:"dir"`
	Entries []FileEntry `json:"entries"`
}

// realRoot is the files folder with symlinks resolved.
func (a *App) realRoot() (string, error) {
	if a.fileRoot == "" {
		return "", i18n.Error("서버 파일 탐색은 웹 버전에서만 쓸 수 있습니다")
	}
	return filepath.EvalSymlinks(a.fileRoot)
}

// inRoot resolves p (relative paths are taken from the files folder) and
// checks it is inside the folder, symlinks included. The file itself need
// not exist yet (a new SQLite database).
func (a *App) inRoot(p string) (string, error) {
	root, err := a.realRoot()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(a.fileRoot, p)
	}
	p = filepath.Clean(p)
	real, err := filepath.EvalSymlinks(p)
	if errors.Is(err, os.ErrNotExist) {
		var dir string
		if dir, err = filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
			real = filepath.Join(dir, filepath.Base(p))
		}
	}
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", i18n.Errorf("웹 버전에서는 파일 폴더(%s) 안의 파일만 쓸 수 있습니다", a.fileRoot)
	}
	return real, nil
}

// BrowseFiles lists a folder inside the files folder ("" for the folder
// itself; a file path lists the folder it is in).
func (a *App) BrowseFiles(dir string) (FileList, error) {
	root, err := a.realRoot()
	if err != nil {
		return FileList{}, err
	}
	if dir == "" {
		dir = root
	}
	d, err := a.inRoot(dir)
	if err != nil {
		// e.g. a path typed for another machine: start at the top instead
		d = root
	}
	if st, err := os.Stat(d); err != nil || !st.IsDir() {
		d = filepath.Dir(d)
		if _, err := a.inRoot(d); err != nil {
			d = root
		}
	}
	ents, err := os.ReadDir(d)
	if err != nil {
		return FileList{}, err
	}
	out := FileList{Root: root, Dir: d, Entries: []FileEntry{}}
	for _, e := range ents {
		info, err := e.Info()
		if err != nil {
			continue
		}
		isDir := info.IsDir()
		if info.Mode()&os.ModeSymlink != 0 {
			st, err := os.Stat(filepath.Join(d, e.Name()))
			if err != nil {
				continue
			}
			isDir = st.IsDir()
		}
		out.Entries = append(out.Entries, FileEntry{Name: e.Name(), Dir: isDir, Size: info.Size(), Modified: info.ModTime()})
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		x, y := out.Entries[i], out.Entries[j]
		if x.Dir != y.Dir {
			return x.Dir
		}
		return strings.ToLower(x.Name) < strings.ToLower(y.Name)
	})
	return out, nil
}

// checkFiles keeps the server files a connection reads (SQLite database,
// SSH key) inside the files folder in the browser build.
func (a *App) checkFiles(c *dbx.ConnConfig) error {
	if a.fileRoot == "" {
		return nil
	}
	if c.Driver == "sqlite" && c.Database != "" && c.Database != ":memory:" {
		p, err := a.inRoot(c.Database)
		if err != nil {
			return err
		}
		c.Database = p
	}
	if c.SSH && c.SSHAuth == "key" {
		// (the desktop app falls back to ~/.ssh/id_*: the server's own keys)
		if strings.TrimSpace(c.SSHKeyFile) == "" {
			return i18n.Error("SSH 키 파일을 고르세요 (웹 버전은 파일 폴더 안의 키만 씁니다)")
		}
		p, err := a.inRoot(c.SSHKeyFile)
		if err != nil {
			return err
		}
		c.SSHKeyFile = p
	}
	return nil
}
