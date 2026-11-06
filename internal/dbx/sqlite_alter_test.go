package dbx

import (
	"path/filepath"
	"strings"
	"testing"
)

// SQLite in the structure view: make a table, then add, rename and drop
// columns; what it can't do is refused before anything runs.
func TestSQLiteStructure(t *testing.T) {
	m := NewManager()
	cfg := ConnConfig{ID: "s", Driver: "sqlite", Database: filepath.Join(t.TempDir(), "s.db")}
	if _, err := m.Connect(cfg); err != nil {
		t.Fatal(err)
	}
	defer m.Disconnect(cfg.ID)
	tbl := Table{Name: "made_t", Type: "TABLE"}
	cols := func() map[string]Column {
		cs, err := m.Columns(cfg.ID, "main", tbl)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]Column{}
		for _, c := range cs {
			out[c.Name] = c
		}
		return out
	}

	id := Column{Name: "id", Type: "integer", PK: true, Extra: "auto_increment"}
	req := CreateRequest{ConnID: cfg.ID, Schema: "main", Table: tbl, Columns: []Column{
		id, {Name: "name", Type: "text"}, {Name: "note", Type: "text", Nullable: true, Default: "'x'"}}}
	for _, bad := range []func(r *CreateRequest){
		func(r *CreateRequest) { r.Table.Comment = "c" },
		func(r *CreateRequest) { r.Columns[1].Comment = "c" },
		func(r *CreateRequest) { r.Columns[0].Type = "bigint" },                         // AUTOINCREMENT is INTEGER only
		func(r *CreateRequest) { r.Columns[1].PK, r.Columns[1].Nullable = true, false }, // and the one key
	} {
		r := req
		r.Columns = append([]Column(nil), req.Columns...)
		bad(&r)
		if _, err := m.PreviewCreateTable(r); err == nil {
			t.Errorf("want refused: %+v", r)
		}
	}
	prev, err := m.PreviewCreateTable(req)
	if err != nil || !strings.Contains(prev[0], `"id" integer PRIMARY KEY AUTOINCREMENT`) || strings.Contains(prev[0], "PRIMARY KEY (") {
		t.Fatalf("%q %v", prev, err)
	}
	if r, err := m.CreateTable(req); err != nil || r.Result.Error != "" {
		t.Fatalf("create: %+v %v", r, err)
	}
	if c := cols(); !c["id"].PK || c["name"].Nullable || !c["note"].Nullable || c["note"].Default != "'x'" {
		t.Fatalf("made: %+v", c)
	}

	c := cols()
	rename := ColumnAlter{Old: c["name"], New: c["name"]}
	rename.New.Name = "title"
	alt := AlterRequest{ConnID: cfg.ID, Schema: "main", Table: tbl,
		Changes: []ColumnAlter{rename}, Add: []Column{{Name: "added", Type: "int", Default: "0"}}, Drop: []string{"note"}}
	if r, err := m.ApplyAlter(alt); err != nil || r.Error != "" {
		t.Fatalf("alter: %+v %v", r, err)
	}
	c = cols()
	if _, ok := c["title"]; !ok || c["added"].Default != "0" || c["added"].Nullable || len(c) != 3 {
		t.Fatalf("after alter: %+v", c)
	}

	typ := ColumnAlter{Old: c["title"], New: c["title"]}
	typ.New.Type = "int"
	empty := ""
	for _, bad := range []AlterRequest{
		{Changes: []ColumnAlter{typ}},
		{Add: []Column{{Name: "k", Type: "integer", PK: true}}},
		{Add: []Column{{Name: "a", Type: "integer", Extra: "auto_increment"}}},
		{Add: []Column{{Name: "c", Type: "text", Nullable: true, Comment: "c"}}},
		{TableComment: func() *string { s := "c"; return &s }()},
	} {
		bad.ConnID, bad.Schema, bad.Table = cfg.ID, "main", tbl
		if _, err := m.PreviewAlter(bad); err == nil {
			t.Errorf("want refused: %+v", bad)
		}
	}
	// no comment to set is not a change
	if _, err := m.PreviewAlter(AlterRequest{ConnID: cfg.ID, Schema: "main", Table: tbl, TableComment: &empty}); err != nil {
		t.Errorf("empty comment: %v", err)
	}
}
