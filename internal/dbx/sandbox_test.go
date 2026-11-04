//go:build !no_sqlite

package dbx

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The browser build's SQLite connections can't open or write other files
// with SQL: ATTACH and VACUUM INTO are refused, and so are the pragmas that
// move temporary files.
func TestSQLiteSandbox(t *testing.T) {
	SQLiteSandbox = true
	defer func() { SQLiteSandbox = false }()
	root, outside := t.TempDir(), t.TempDir()
	db, err := sql.Open("sqlite", sqliteDialect{}.DSN(ConnConfig{Database: filepath.Join(root, "app.db"), Params: "_pragma=foreign_keys(1)"}))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{"CREATE TABLE t(a)", "INSERT INTO t VALUES (1)", "ATTACH ':memory:' AS m", "CREATE TABLE m.x(a)", "DETACH m", "VACUUM", "PRAGMA foreign_keys"} {
		if _, err := db.Exec(q); err != nil {
			t.Errorf("%s: %v", q, err)
		}
	}
	out := filepath.Join(outside, "x.db")
	for _, q := range []string{
		"ATTACH '" + out + "' AS x",
		"attach database 'file:" + out + "?mode=rwc' as y",
		"/* c */ VACUUM INTO '" + out + "'",
		"VACUUM main INTO '" + filepath.Join(root, "copy.db") + "'", // (inside too: any other file)
		"PRAGMA temp_store_directory = '" + outside + "'",
	} {
		if _, err := db.Exec(q); err == nil || !strings.Contains(err.Error(), "authoriz") {
			t.Errorf("%s: want refused, got %v", q, err)
		}
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Errorf("files made outside: %v", ents)
	}
}

// Characters a file: URI reads differently stay part of the file name.
func TestSQLiteDSNPath(t *testing.T) {
	root := t.TempDir()
	for _, c := range []ConnConfig{
		{Database: filepath.Join(root, "a%2e%2e.db"), Params: "_pragma=foreign_keys(1)"},
		{Database: filepath.Join(root, "b?mode=memory.db")},
		{Database: filepath.Join(root, "c#d.db"), Params: "cache=shared"},
	} {
		db, err := sql.Open("sqlite", sqliteDialect{}.DSN(c))
		if err == nil {
			_, err = db.Exec("CREATE TABLE t(a)")
			db.Close()
		}
		if err != nil {
			t.Fatalf("%s: %v", c.Database, err)
		}
		if _, err := os.Stat(c.Database); err != nil {
			t.Errorf("%s not made: %v", c.Database, err)
		}
	}
}
