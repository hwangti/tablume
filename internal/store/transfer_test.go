package store

import (
	"encoding/json"
	"strings"
	"testing"

	"tablume/internal/dbx"
)

func TestExportImport(t *testing.T) {
	NoKeyring = true
	DirOverride = t.TempDir()
	defer func() { NoKeyring, DirOverride = false, "" }()

	s, _ := Open()
	a, _ := s.Save(dbx.ConnConfig{Name: "prod", Driver: "mysql", Host: "db", Port: 3306, User: "app", Password: "hunter2", SavePassword: true,
		SSH: true, SSHHost: "bastion", SSHPort: 22, SSHUser: "me", SSHAuth: "password", SSHPassword: "pass phrase", Color: "#f00",
		Schemas: &dbx.SchemaFilter{Names: []string{"shop"}}})
	s.Save(dbx.ConnConfig{Name: "local", Driver: "sqlite", Database: "/tmp/x.db"})

	b, err := s.Export(nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "hunter2") || strings.Contains(string(b), "pass phrase") || strings.Contains(string(b), a.ID) || strings.Contains(string(b), `"id"`) || strings.Contains(string(b), "savePassword") {
		t.Fatalf("export holds a password or an id:\n%s", b)
	}
	one, _ := s.Export([]string{a.ID})
	var f exportFile
	json.Unmarshal(one, &f)
	if len(f.Connections) != 1 || f.Connections[0].Name != "prod" {
		t.Fatalf("export of one: %+v", f.Connections)
	}

	// into the same install: nothing new
	r, err := s.Import(b, []string{"mysql", "sqlite"})
	if err != nil || r.Added != 0 || r.Existing != 2 {
		t.Fatalf("re-import: %+v %v", r, err)
	}

	// into another
	DirOverride = t.TempDir()
	s2, _ := Open()
	r, err = s2.Import(b, []string{"mysql"})
	if err != nil || r.Added != 1 || len(r.Unsupported) != 1 || r.Unsupported[0] != "local" {
		t.Fatalf("import: %+v %v", r, err)
	}
	got := s2.List()[0]
	if got.ID == "" || got.ID == a.ID || got.SavePassword || got.Color != "#f00" || got.SSHHost != "bastion" || got.Schemas == nil || got.Schemas.Names[0] != "shop" {
		t.Fatalf("imported: %+v", got)
	}
	if c, _ := s2.Get(got.ID); c.Password != "" || c.SSHPassword != "" {
		t.Fatal("imported a password")
	}

	// a copy of connections.json; one with a name already used but another server is added
	raw := `[{"id":"x","name":"prod","driver":"mysql","host":"db2","port":3306,"user":"app","fallbackPassword":"zz"}]`
	if r, err = s2.Import([]byte(raw), []string{"mysql"}); err != nil || r.Added != 1 {
		t.Fatalf("raw import: %+v %v", r, err)
	}

	for _, bad := range []string{`{"format":"other"}`, `{"format":"tablume-connections","version":2}`, `nope`} {
		if _, err := s2.Import([]byte(bad), []string{"mysql"}); err == nil {
			t.Errorf("Import(%s) took it", bad)
		}
	}
}
