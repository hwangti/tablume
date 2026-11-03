package main

import (
	"os"
	"path/filepath"
	"testing"

	"tablume/internal/dbx"
)

// The browser build reads files only inside its files folder.
func TestFilesStayInTheFilesFolder(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(root, "app.db"), nil, 0o600)
	os.Mkdir(filepath.Join(root, "sub"), 0o700)
	os.WriteFile(filepath.Join(outside, "secret"), nil, 0o600)
	// a link inside pointing out
	os.Symlink(outside, filepath.Join(root, "sub", "out"))

	a := &App{fileRoot: root}
	ok := []string{"app.db", "new.db", filepath.Join(root, "app.db"), filepath.Join(root, "sub", "x.db")}
	for _, p := range ok {
		if _, err := a.inRoot(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	bad := []string{"/etc/passwd", "../x.db", filepath.Join(root, "..", "x"), filepath.Join(root, "sub", "out", "secret"), filepath.Join(outside, "secret")}
	for _, p := range bad {
		if _, err := a.inRoot(p); err == nil {
			t.Errorf("%s: allowed", p)
		}
	}

	c := dbx.ConnConfig{Driver: "sqlite", Database: "app.db"}
	if err := a.checkFiles(&c); err != nil || !filepath.IsAbs(c.Database) {
		t.Errorf("relative sqlite path: %v %q", err, c.Database)
	}
	c = dbx.ConnConfig{Driver: "mysql", SSH: true, SSHAuth: "key"}
	if err := a.checkFiles(&c); err == nil {
		t.Error("empty SSH key file allowed (would read the server's ~/.ssh)")
	}

	l, err := a.BrowseFiles(filepath.Join(root, "sub", "out"))
	if err != nil || l.Dir == outside {
		t.Errorf("browsing through a link out: %v %q", err, l.Dir)
	}

	// the desktop app has no files folder
	if _, err := (&App{}).BrowseFiles(""); err == nil {
		t.Error("desktop BrowseFiles should fail")
	}
}
