package dbx

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Run with TABLUME_IT=1 against the docker containers described in README.
func targets(t *testing.T) map[string]struct {
	cfg    ConnConfig
	schema string
	setup  string
	table  string
} {
	if os.Getenv("TABLUME_IT") == "" {
		t.Skip("set TABLUME_IT=1 to run integration tests")
	}
	type tg = struct {
		cfg    ConnConfig
		schema string
		setup  string
		table  string
	}
	return map[string]tg{
		"mariadb": {ConnConfig{ID: "m", Driver: "mariadb", Host: "127.0.0.1", User: "root", Password: "pw", Database: "shop"}, "shop",
			"DROP TABLE IF EXISTS items; CREATE TABLE items (id INT PRIMARY KEY, name VARCHAR(50) NOT NULL, price DECIMAL(20,4), big BIGINT, at DATETIME(3), d DATE, b BLOB, j JSON); " +
				"INSERT INTO items VALUES (1,'한글;이름',12345678901234.5678,9223372036854775807,'2024-01-02 03:04:05.678','2024-05-06',x'00FF','{\"a\":1}'),(2,'b',NULL,NULL,NULL,NULL,NULL,NULL),(3,'c',1,1,NULL,NULL,NULL,NULL)", "items"},
		"postgres": {ConnConfig{ID: "p", Driver: "postgres", Host: "127.0.0.1", User: "postgres", Password: "pw"}, "public",
			"DROP TABLE IF EXISTS items; CREATE TABLE items (id int primary key, name varchar(50) not null, price numeric(30,4), big bigint, at timestamptz, d date, b bytea, u uuid, j jsonb, arr int[]); " +
				"INSERT INTO items VALUES (1,'한글;이름',12345678901234567890.5678,9223372036854775807,'2024-01-02 03:04:05.678+09','2024-05-06','\\x00ff','a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11','{\"a\":1}','{1,2}'),(2,'b',null,null,null,null,null,null,null,null),(3,'c',1,1,null,null,null,null,null,null)", "items"},
		"mssql": {ConnConfig{ID: "s", Driver: "mssql", Host: "127.0.0.1", User: "sa", Password: "Passw0rd!", Database: "master"}, "master",
			"IF OBJECT_ID('dbo.items') IS NOT NULL DROP TABLE dbo.items;\nCREATE TABLE dbo.items (id int primary key, name nvarchar(50) not null, price decimal(20,4), big bigint, at datetime2, d date, b varbinary(10), u uniqueidentifier, m money);\n" +
				"INSERT INTO dbo.items VALUES (1,N'한글;이름',12345678901234.5678,9223372036854775807,'2024-01-02 03:04:05.678','2024-05-06',0x00FF,'A0EEBC99-9C0B-4EF8-BB6D-6BB9BD380A11',12.34),(2,'b',null,null,null,null,null,null,null),(3,'c',1,1,null,null,null,null,null)", "items"},
		"oracle": {ConnConfig{ID: "o", Driver: "oracle", Host: "127.0.0.1", User: "app", Password: "pw", Database: "FREEPDB1"}, "APP",
			"BEGIN EXECUTE IMMEDIATE 'DROP TABLE items'; EXCEPTION WHEN OTHERS THEN NULL; END;\n/\n" +
				"CREATE TABLE items (id NUMBER PRIMARY KEY, name VARCHAR2(50) NOT NULL, price NUMBER(30,4), big NUMBER(19), at TIMESTAMP, d DATE, b RAW(10), c CLOB);\n" +
				"INSERT INTO items VALUES (1,'한글;이름',12345678901234567890.5678,9223372036854775807,TIMESTAMP '2024-01-02 03:04:05.678',DATE '2024-05-06',HEXTORAW('00FF'),'long text');\n" +
				"INSERT INTO items (id,name) VALUES (2,'b');\nINSERT INTO items (id,name,price,big) VALUES (3,'c',1,1);\nCOMMIT", "ITEMS"},
		"sqlite": {ConnConfig{ID: "l", Driver: "sqlite", Database: filepath.Join(t.TempDir(), "x.db")}, "main",
			"CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL, price REAL, big INTEGER, at DATETIME, d DATE, b BLOB); " +
				"INSERT INTO items VALUES (1,'한글;이름',1.5,9223372036854775807,'2024-01-02 03:04:05','2024-05-06',x'00FF'),(2,'b',null,null,null,null,null),(3,'c',1,1,null,null,null)", "items"},
	}
}

func TestIntegration(t *testing.T) {
	for name, tg := range targets(t) {
		t.Run(name, func(t *testing.T) {
			m := NewManager()
			ver, err := m.Connect(tg.cfg)
			if err != nil {
				t.Fatal("connect:", err)
			}
			t.Log("version:", ver)
			defer m.Disconnect(tg.cfg.ID)

			res, err := m.Execute(ExecRequest{TabID: "t1", ConnID: tg.cfg.ID, Schema: tg.schema, SQL: tg.setup, Cursor: -1})
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range res.Results {
				if r.Error != "" {
					t.Fatalf("setup %q: %s", r.SQL, r.Error)
				}
			}

			// caret inside the second statement; fetch 2 then load more
			script := "SELECT 1 AS x FROM items;\nSELECT * FROM items ORDER BY id"
			if name == "mssql" {
				script = "SELECT 1 AS x FROM dbo.items;\nSELECT * FROM dbo.items ORDER BY id"
			}
			res, err = m.Execute(ExecRequest{TabID: "t1", ConnID: tg.cfg.ID, Schema: tg.schema, SQL: script, Cursor: len(script) - 3, Fetch: 2})
			if err != nil {
				t.Fatal(err)
			}
			r := res.Results[0]
			if r.Error != "" {
				t.Fatal(r.Error)
			}
			if !strings.HasPrefix(r.SQL, "SELECT *") || len(r.Rows) != 2 || !r.HasMore || !r.Paged {
				t.Fatalf("unexpected first page: %+v", r)
			}
			var cols []string
			for _, c := range r.Columns {
				cols = append(cols, c.Name+":"+c.Type)
			}
			t.Log("columns:", cols)
			t.Logf("row1: %q", r.Rows[0])
			t.Logf("row2: %q", r.Rows[1])
			if r.Rows[0][1] != "한글;이름" {
				t.Errorf("korean text: %q", r.Rows[0][1])
			}
			if r.Rows[1][2] != nil {
				t.Errorf("expected NULL, got %q", r.Rows[1][2])
			}
			more, err := m.FetchPage("t1", tg.cfg.ID, r.SQL, 2, 10)
			if err != nil || len(more.Rows) != 1 || more.HasMore {
				t.Fatalf("fetch more: %v %+v", err, more)
			}

			// a new run while a cursor is open must not hang
			res, err = m.Execute(ExecRequest{TabID: "t1", ConnID: tg.cfg.ID, SQL: "UPDATE items SET name = name WHERE id < 3", Cursor: -1})
			if name == "mssql" {
				res, err = m.Execute(ExecRequest{TabID: "t1", ConnID: tg.cfg.ID, SQL: "UPDATE dbo.items SET name = name WHERE id < 3", Cursor: -1})
			}
			if err != nil || res.Results[0].Error != "" || res.Results[0].RowsAffected != 2 {
				t.Fatalf("update: %v %+v", err, res.Results)
			}
			if res.CurrentSchema == "" {
				t.Error("no current schema")
			}

			// error reporting stops the batch
			res, _ = m.Execute(ExecRequest{TabID: "t1", ConnID: tg.cfg.ID, SQL: "SELECT * FROM no_such_table; SELECT 1", Cursor: -1})
			if len(res.Results) != 1 || res.Results[0].Error == "" {
				t.Fatalf("expected one error result: %+v", res.Results)
			}
			t.Log("error text:", res.Results[0].Error)

			sch, err := m.Schemas(tg.cfg.ID)
			if err != nil {
				t.Fatal(err)
			}
			t.Log("schemas:", sch.Schemas, "current:", sch.Current)
			tables, err := m.Tables(tg.cfg.ID, tg.schema)
			if err != nil {
				t.Fatal(err)
			}
			var found *Table
			for i := range tables {
				if tables[i].Name == tg.table {
					found = &tables[i]
				}
			}
			if found == nil {
				t.Fatalf("table not listed: %+v", tables)
			}
			colsInfo, err := m.Columns(tg.cfg.ID, tg.schema, *found)
			if err != nil || len(colsInfo) < 5 || !colsInfo[0].PK || colsInfo[1].Nullable || !colsInfo[2].Nullable {
				t.Fatalf("columns: %v %+v", err, colsInfo)
			}
			t.Logf("columns meta: %+v", colsInfo[:3])
			all, err := m.AllColumns(tg.cfg.ID, tg.schema)
			if err != nil || len(all[tg.table]) < 5 {
				t.Fatalf("all columns: %v %v", err, all[tg.table])
			}
			q, _ := m.SelectSQL(tg.cfg.ID, tg.schema, *found, "", "", 100, 0)
			res, err = m.Execute(ExecRequest{TabID: "t2", ConnID: tg.cfg.ID, SQL: q, Cursor: -1})
			if err != nil || res.Results[0].Error != "" || len(res.Results[0].Rows) != 3 {
				t.Fatalf("select top %q: %v %+v", q, err, res.Results)
			}
			// filtered + sorted + limited, as the table data filter bar builds it
			idCol, _ := m.QuoteIdent(tg.cfg.ID, colsInfo[0].Name)
			q, _ = m.SelectSQL(tg.cfg.ID, tg.schema, *found, idCol+" >= 2", idCol+" DESC", 1, 0)
			res, err = m.Execute(ExecRequest{TabID: "t2", ConnID: tg.cfg.ID, SQL: q, Cursor: -1})
			if err != nil || res.Results[0].Error != "" || len(res.Results[0].Rows) != 1 || res.Results[0].Rows[0][0] != "3" {
				t.Fatalf("filtered select %q: %v %+v", q, err, res.Results)
			}
			t.Log("filtered:", q)

			// pages: ORDER BY id LIMIT 1 OFFSET 1 -> id 2; no ORDER BY, 2 rows after 1
			q, _ = m.SelectSQL(tg.cfg.ID, tg.schema, *found, "", idCol, 1, 1)
			res, err = m.Execute(ExecRequest{TabID: "t2", ConnID: tg.cfg.ID, SQL: q, Cursor: -1})
			if err != nil || res.Results[0].Error != "" || len(res.Results[0].Rows) != 1 || res.Results[0].Rows[0][0] != "2" {
				t.Fatalf("page %q: %v %+v", q, err, res.Results)
			}
			q, _ = m.SelectSQL(tg.cfg.ID, tg.schema, *found, "", "", 2, 1)
			res, err = m.Execute(ExecRequest{TabID: "t2", ConnID: tg.cfg.ID, SQL: q, Cursor: -1})
			if err != nil || res.Results[0].Error != "" || len(res.Results[0].Rows) != 2 {
				t.Fatalf("page without order %q: %v %+v", q, err, res.Results)
			}
			q, _ = m.SelectSQL(tg.cfg.ID, tg.schema, *found, "", "", 0, 2)
			res, err = m.Execute(ExecRequest{TabID: "t2", ConnID: tg.cfg.ID, SQL: q, Cursor: -1})
			if err != nil || res.Results[0].Error != "" || len(res.Results[0].Rows) != 1 {
				t.Fatalf("offset only %q: %v %+v", q, err, res.Results)
			}
			if n, err := m.CountRows(tg.cfg.ID, tg.schema, *found, ""); err != nil || n != 3 {
				t.Fatalf("count: %d %v", n, err)
			}
			if n, err := m.CountRows(tg.cfg.ID, tg.schema, *found, idCol+" >= 2"); err != nil || n != 2 {
				t.Fatalf("count where: %d %v", n, err)
			}

			// indexes: the primary key is listed first; create, list, drop
			ixs, err := m.ListIndexes(tg.cfg.ID, tg.schema, *found)
			if err != nil {
				t.Fatal("indexes:", err)
			}
			t.Logf("indexes: %+v", ixs)
			if name != "sqlite" && (len(ixs) == 0 || !ixs[0].Primary) { // sqlite: INTEGER PRIMARY KEY is the rowid
				t.Fatalf("primary key index missing: %+v", ixs)
			}
			ixName := "ix_items_name_price"
			if name == "oracle" {
				ixName = strings.ToUpper(ixName)
			}
			req := IndexRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: *found,
				Create: []NewIndex{{Name: ixName, Unique: true, Columns: []IndexColumn{{Name: colsInfo[1].Name}, {Name: colsInfo[2].Name, Desc: true}}}}}
			stmts, err := m.PreviewIndexes(req)
			t.Log("create:", stmts, err)
			if r, err := m.ApplyIndexes(req); err != nil || r.Error != "" || r.Applied != 1 {
				t.Fatalf("create index: %v %+v", err, r)
			}
			ixs, _ = m.ListIndexes(tg.cfg.ID, tg.schema, *found)
			var made *Index
			for i := range ixs {
				if ixs[i].Name == ixName {
					made = &ixs[i]
				}
			}
			if made == nil || !made.Unique || len(made.Columns) != 2 || !strings.HasSuffix(made.Columns[1], " DESC") ||
				!strings.EqualFold(strings.Trim(made.Columns[0], `"`), colsInfo[1].Name) {
				t.Fatalf("created index: %+v", ixs)
			}
			req = IndexRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: *found, Drop: []Index{*made}}
			if r, err := m.ApplyIndexes(req); err != nil || r.Error != "" || r.Applied != 1 {
				t.Fatalf("drop index: %v %+v", err, r)
			}
			if again, _ := m.ListIndexes(tg.cfg.ID, tg.schema, *found); len(again) != len(ixs)-1 {
				t.Fatalf("index not dropped: %+v", again)
			}
			// rename and back
			newName := "items_renamed"
			if name == "oracle" {
				newName = "ITEMS_RENAMED"
			}
			st, err := m.RenameTable(tg.cfg.ID, tg.schema, *found, newName)
			t.Log("rename:", st)
			if err != nil {
				t.Fatal("rename:", err)
			}
			renamed := *found
			renamed.Name = newName
			if _, err := m.RenameTable(tg.cfg.ID, tg.schema, renamed, found.Name); err != nil {
				t.Fatal("rename back:", err)
			}
			if len(ixs) > 0 && ixs[0].Primary {
				if _, err := m.PreviewIndexes(IndexRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: *found, Drop: ixs[:1]}); err == nil {
					t.Fatal("dropping the primary key must be refused")
				}
			}
		})
	}
}

func TestCursorAndCancel(t *testing.T) {
	all := targets(t)
	cases := map[string][2]string{
		// queries that limit themselves can't be paged: they keep a cursor
		"mariadb":  {"SELECT * FROM seq_1_to_50000000 LIMIT 50000000", "SELECT SLEEP(20)"},
		"postgres": {"SELECT * FROM generate_series(1, 50000000) LIMIT 50000000", "SELECT pg_sleep(20)"},
		"mssql":    {"SELECT TOP 50000000 a.object_id FROM sys.all_objects a CROSS JOIN sys.all_objects b CROSS JOIN sys.all_objects c", "WAITFOR DELAY '00:00:20'"},
		"oracle":   {"SELECT level FROM dual CONNECT BY level <= 50000000 FETCH FIRST 50000000 ROWS ONLY", "BEGIN DBMS_SESSION.SLEEP(20); END;"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := all[name].cfg
			m := NewManager()
			if _, err := m.Connect(cfg); err != nil {
				t.Fatal(err)
			}
			defer m.Disconnect(cfg.ID)
			res, err := m.Execute(ExecRequest{TabID: "c", ConnID: cfg.ID, SQL: c[0], Cursor: -1, Fetch: 10})
			if err != nil || res.Results[0].Error != "" || !res.Results[0].HasMore || res.Results[0].Paged || res.Results[0].CursorID == "" {
				t.Fatalf("%v %+v", err, res.Results)
			}
			start := time.Now()
			res, err = m.Execute(ExecRequest{TabID: "c", ConnID: cfg.ID, SQL: "SELECT 1" + map[bool]string{true: " FROM dual"}[name == "oracle"], Cursor: -1})
			if err != nil || res.Results[0].Error != "" {
				t.Fatalf("after abandoned cursor: %v %+v", err, res.Results)
			}
			t.Log("abandon cursor took", time.Since(start))
			if time.Since(start) > 5*time.Second {
				t.Error("too slow")
			}
			go func() { time.Sleep(time.Second); m.Cancel("c") }()
			start = time.Now()
			res, err = m.Execute(ExecRequest{TabID: "c", ConnID: cfg.ID, SQL: c[1], Cursor: -1})
			t.Log("cancel took", time.Since(start), err, res.Results[0].Error)
			if time.Since(start) > 8*time.Second {
				t.Error("cancel too slow")
			}
			res, err = m.Execute(ExecRequest{TabID: "c", ConnID: cfg.ID, SQL: "SELECT 2" + map[bool]string{true: " FROM dual"}[name == "oracle"], Cursor: -1})
			if err != nil || res.Results[0].Error != "" || res.Results[0].Rows[0][0] != "2" {
				t.Fatalf("after cancel: %v %+v", err, res.Results)
			}
		})
	}
}

func TestOracleCompileErrors(t *testing.T) {
	cfg := targets(t)["oracle"].cfg
	m := NewManager()
	if _, err := m.Connect(cfg); err != nil {
		t.Fatal(err)
	}
	defer m.Disconnect(cfg.ID)
	res, _ := m.Execute(ExecRequest{TabID: "x", ConnID: cfg.ID, SQL: "-- c\nCREATE OR REPLACE PROCEDURE broken AS BEGIN nosuch(); END;", Cursor: -1})
	if !strings.Contains(res.Results[0].Error, "PLS-") {
		t.Fatalf("expected compile error, got %+v", res.Results[0])
	}
	t.Log(res.Results[0].Error)
	res, _ = m.Execute(ExecRequest{TabID: "x", ConnID: cfg.ID, SQL: "CREATE OR REPLACE PROCEDURE fine AS BEGIN NULL; END;", Cursor: -1})
	if res.Results[0].Error != "" {
		t.Fatal(res.Results[0].Error)
	}
}

func TestDDLAndEditing(t *testing.T) {
	all := targets(t)
	for _, name := range []string{"mariadb", "postgres", "mssql", "oracle", "sqlite"} {
		tg := all[name]
		t.Run(name, func(t *testing.T) {
			m := NewManager()
			if _, err := m.Connect(tg.cfg); err != nil {
				t.Fatal(err)
			}
			defer m.Disconnect(tg.cfg.ID)
			run := func(q string) StmtResult {
				t.Helper()
				res, err := m.Execute(ExecRequest{TabID: "e", ConnID: tg.cfg.ID, Schema: tg.schema, SQL: q, Cursor: -1})
				if err != nil {
					t.Fatal(err)
				}
				last := res.Results[len(res.Results)-1]
				if last.Error != "" {
					t.Fatalf("%s: %s", q, last.Error)
				}
				return last
			}
			run(tg.setup)
			tables, _ := m.Tables(tg.cfg.ID, tg.schema)
			var tbl Table
			for _, x := range tables {
				if x.Name == tg.table {
					tbl = x
				}
			}
			ddl, err := m.DDL(tg.cfg.ID, tg.schema, tbl)
			if err != nil {
				t.Fatal("ddl:", err)
			}
			t.Logf("DDL:\n%s", ddl)

			sel := "SELECT * FROM " + tg.table + " ORDER BY id"
			if name == "mssql" {
				sel = "SELECT * FROM dbo.items ORDER BY id"
			}
			r := run(sel)
			if r.Source == nil {
				t.Fatal("no source detected")
			}
			var cols []string
			for _, c := range r.Columns {
				cols = append(cols, c.Name)
			}
			info, err := m.EditInfo(tg.cfg.ID, tg.schema, r.Source, cols)
			if err != nil || !info.Editable {
				t.Fatalf("edit info: %v %+v", err, info)
			}
			t.Logf("edit info: key=%v cols=%v", info.Key, info.Columns)
			col := func(i int) string { return info.Columns[i] }
			s := func(v string) *string { return &v }
			// 1: change text (with a quote) and a date; 2: number + NULL; 3: delete; 4: insert
			changes := []RowChange{
				{Kind: "update", Key: map[string]any{col(0): s("1")}, Values: map[string]any{col(1): s("O'Brien 한글"), col(5): s("2025-12-31")}},
				{Kind: "update", Key: map[string]any{col(0): s("2")}, Values: map[string]any{col(2): s("42.5"), col(3): nil}},
				{Kind: "delete", Key: map[string]any{col(0): s("3")}},
				{Kind: "insert", Values: map[string]any{col(0): s("4"), col(1): s("new"), col(4): s("2024-02-03 04:05:06")}},
			}
			req := ApplyRequest{ConnID: tg.cfg.ID, Schema: info.Schema, Table: info.Table, Changes: changes}
			preview, err := m.PreviewChanges(req)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("preview:\n%s", strings.Join(preview, "\n"))
			if n, err := m.ApplyChanges(req); err != nil || n != 4 {
				t.Fatalf("apply: %d %v", n, err)
			}
			r = run(sel)
			if len(r.Rows) != 3 {
				t.Fatalf("rows after apply: %q", r.Rows)
			}
			t.Logf("after: %q", r.Rows)
			if r.Rows[0][1] != "O'Brien 한글" || !strings.HasPrefix(r.Rows[0][5].(string), "2025-12-31") {
				t.Errorf("row 1 not updated: %q", r.Rows[0])
			}
			if !strings.HasPrefix(r.Rows[1][2].(string), "42.5") || r.Rows[1][3] != nil {
				t.Errorf("row 2 not updated: %q", r.Rows[1])
			}
			if r.Rows[2][0] != "4" || !strings.HasPrefix(r.Rows[2][4].(string), "2024-02-03 04:05:06") {
				t.Errorf("row 4 not inserted: %q", r.Rows[2])
			}
			// a change that matches no row rolls everything back
			bad := ApplyRequest{ConnID: tg.cfg.ID, Schema: info.Schema, Table: info.Table, Changes: []RowChange{
				{Kind: "update", Key: map[string]any{col(0): s("1")}, Values: map[string]any{col(1): s("should roll back")}},
				{Kind: "delete", Key: map[string]any{col(0): s("999")}},
			}}
			if _, err := m.ApplyChanges(bad); err == nil {
				t.Fatal("expected failure")
			} else {
				t.Log("expected error:", err)
			}
			r = run(sel)
			if r.Rows[0][1] != "O'Brien 한글" {
				t.Errorf("not rolled back: %q", r.Rows[0])
			}
		})
	}
}

func TestTableMeta(t *testing.T) {
	all := targets(t)
	comments := map[string]string{
		"mariadb":  "ALTER TABLE items COMMENT = '상품 테이블'; ALTER TABLE items MODIFY name VARCHAR(50) NOT NULL COMMENT '상품명'; ANALYZE TABLE items",
		"postgres": "COMMENT ON TABLE items IS '상품 테이블'; COMMENT ON COLUMN items.name IS '상품명'; ANALYZE items",
		"mssql": "EXEC sp_addextendedproperty 'MS_Description', N'상품 테이블', 'SCHEMA', 'dbo', 'TABLE', 'items';\n" +
			"EXEC sp_addextendedproperty 'MS_Description', N'상품명', 'SCHEMA', 'dbo', 'TABLE', 'items', 'COLUMN', 'name'",
		"oracle": "COMMENT ON TABLE items IS '상품 테이블';\nCOMMENT ON COLUMN items.name IS '상품명';\nBEGIN DBMS_STATS.GATHER_TABLE_STATS(USER, 'ITEMS'); END;\n/",
	}
	for name, cm := range comments {
		tg := all[name]
		t.Run(name, func(t *testing.T) {
			m := NewManager()
			if _, err := m.Connect(tg.cfg); err != nil {
				t.Fatal(err)
			}
			defer m.Disconnect(tg.cfg.ID)
			for _, q := range []string{tg.setup, cm} {
				res, err := m.Execute(ExecRequest{TabID: "m", ConnID: tg.cfg.ID, Schema: tg.schema, SQL: q, Cursor: -1})
				if err != nil || res.Results[len(res.Results)-1].Error != "" {
					t.Fatalf("%v %+v", err, res.Results)
				}
			}
			tables, err := m.Tables(tg.cfg.ID, tg.schema)
			if err != nil {
				t.Fatal(err)
			}
			for _, x := range tables {
				if x.Name == tg.table {
					t.Logf("table: %+v", x)
					if x.Comment != "상품 테이블" || x.Rows != -1 || x.Size != -1 {
						t.Errorf("table meta: %+v", x)
					}
					stats, err := m.TableStats(tg.cfg.ID, tg.schema)
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, st := range stats {
						if st.Name == x.Name && st.Schema == x.Schema {
							found = true
							t.Logf("stats: %+v", st)
							if st.Rows < 0 || st.Size < 0 {
								t.Errorf("stats: %+v", st)
							}
						}
					}
					if !found {
						t.Errorf("no stats for %s", x.Name)
					}
					cols, err := m.Columns(tg.cfg.ID, tg.schema, x)
					if err != nil || cols[1].Comment != "상품명" {
						t.Errorf("column comment: %v %+v", err, cols[1])
					}
				}
			}
		})
	}
}

func TestKeyColumns(t *testing.T) {
	all := targets(t)
	ddl := map[string]string{
		"mariadb":  "DROP TABLE IF EXISTS item_child; CREATE TABLE item_child (id INT PRIMARY KEY, code VARCHAR(20) DEFAULT 'xd' UNIQUE, item_id INT, FOREIGN KEY (item_id) REFERENCES items(id))",
		"postgres": "DROP TABLE IF EXISTS item_child; CREATE TABLE item_child (id int PRIMARY KEY, code varchar(20) DEFAULT 'xd' UNIQUE, item_id int REFERENCES items(id))",
		"mssql":    "IF OBJECT_ID('dbo.item_child') IS NOT NULL DROP TABLE dbo.item_child;\nCREATE TABLE dbo.item_child (id int PRIMARY KEY, code varchar(20) DEFAULT 'xd' UNIQUE, item_id int REFERENCES dbo.items(id))",
		"oracle":   "BEGIN EXECUTE IMMEDIATE 'DROP TABLE item_child'; EXCEPTION WHEN OTHERS THEN NULL; END;\n/\nCREATE TABLE item_child (id NUMBER PRIMARY KEY, code VARCHAR2(20) DEFAULT 'xd' UNIQUE, item_id NUMBER REFERENCES items(id))",
		"sqlite":   "CREATE TABLE item_child (id INTEGER PRIMARY KEY, code TEXT DEFAULT 'xd' UNIQUE, item_id INTEGER REFERENCES items(id))",
	}
	for name, d := range ddl {
		tg := all[name]
		t.Run(name, func(t *testing.T) {
			m := NewManager()
			if _, err := m.Connect(tg.cfg); err != nil {
				t.Fatal(err)
			}
			defer m.Disconnect(tg.cfg.ID)
			// The child references items; drop it so other tests can recreate items.
			defer m.Execute(ExecRequest{TabID: "k", ConnID: tg.cfg.ID, Schema: tg.schema, SQL: "DROP TABLE item_child", Cursor: -1})
			for _, q := range []string{tg.setup, d} {
				res, err := m.Execute(ExecRequest{TabID: "k", ConnID: tg.cfg.ID, Schema: tg.schema, SQL: q, Cursor: -1})
				if err != nil || res.Results[len(res.Results)-1].Error != "" {
					t.Fatalf("%v %+v", err, res.Results)
				}
			}
			name := "item_child"
			if tg.cfg.Driver == "oracle" {
				name = "ITEM_CHILD"
			}
			tbl := Table{Name: name}
			if tg.cfg.Driver == "mssql" {
				tbl.Schema = "dbo"
			}
			cols, err := m.Columns(tg.cfg.ID, tg.schema, tbl)
			if err != nil || len(cols) != 3 {
				t.Fatalf("%v %+v", err, cols)
			}
			t.Logf("%+v", cols)
			if !cols[0].PK || !cols[1].Unique || cols[0].Unique || !strings.HasSuffix(strings.ToLower(cols[2].FK), "items.id") {
				t.Errorf("key flags wrong: %+v", cols)
			}
			if !strings.Contains(cols[1].Default, "xd") {
				t.Errorf("default missing: %+v", cols[1])
			}
		})
	}
}

func TestFloatFormat(t *testing.T) {
	qs := map[string]string{
		"mariadb":  "SELECT CAST(2.611e25 AS DOUBLE), CAST(1.5 AS FLOAT), 0.000123e-2",
		"postgres": "SELECT 2.611e25::float8, 1.5::float4, 0.000123e-2::float8",
		"mssql":    "SELECT CAST(2.611e25 AS float), CAST(1.5 AS real), CAST(0.000123e-2 AS float)",
		"oracle":   "SELECT CAST(2.611e25 AS BINARY_DOUBLE), CAST(1.5 AS BINARY_FLOAT), CAST(0.000123e-2 AS BINARY_DOUBLE) FROM dual",
		"sqlite":   "SELECT 2.611e25, 1.5, 0.000123e-2",
	}
	for name, tg := range targets(t) {
		t.Run(name, func(t *testing.T) {
			m := NewManager()
			if _, err := m.Connect(tg.cfg); err != nil {
				t.Fatal(err)
			}
			defer m.Disconnect(tg.cfg.ID)
			res, err := m.Execute(ExecRequest{TabID: "f", ConnID: tg.cfg.ID, SQL: qs[name], Cursor: -1})
			if err != nil || res.Results[0].Error != "" {
				t.Fatal(err, res.Results[0].Error)
			}
			got := res.Results[0].Rows[0]
			want := []any{"26110000000000000000000000", "1.5", "0.00000123"}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("col %d: got %v want %v (%s)", i, got[i], want[i], res.Results[0].Columns[i].Type)
				}
			}
		})
	}
}

// MySQL LONGTEXT used to be taken for Oracle LONG and made read-only.
func TestLongTextEditable(t *testing.T) {
	tg := targets(t)["mariadb"]
	m := NewManager()
	if _, err := m.Connect(tg.cfg); err != nil {
		t.Fatal(err)
	}
	defer m.Disconnect(tg.cfg.ID)
	defer m.Execute(ExecRequest{TabID: "l", ConnID: tg.cfg.ID, Schema: tg.schema, SQL: "DROP TABLE IF EXISTS art", Cursor: -1})
	res, err := m.Execute(ExecRequest{TabID: "l", ConnID: tg.cfg.ID, Schema: tg.schema, Cursor: -1,
		SQL: "DROP TABLE IF EXISTS art; CREATE TABLE art (id bigint AUTO_INCREMENT PRIMARY KEY, content longtext NOT NULL, j mediumtext); INSERT INTO art (content) VALUES ('<p>a &gt; b</p>')"})
	if err != nil || res.Results[len(res.Results)-1].Error != "" {
		t.Fatal(err, res.Results)
	}
	info, err := m.EditInfo(tg.cfg.ID, tg.schema, []string{"art"}, []string{"id", "content", "j"})
	if err != nil || !info.Editable || info.Columns[1] != "content" || info.Columns[2] != "j" {
		t.Fatalf("%v %+v", err, info)
	}
	n, err := m.ApplyChanges(ApplyRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: info.Table,
		Changes: []RowChange{{Kind: "update", Key: map[string]any{"id": "1"}, Values: map[string]any{"content": "<p>new</p>"}}}})
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
}

func TestPaging(t *testing.T) {
	for name, tg := range targets(t) {
		t.Run(name, func(t *testing.T) {
			m := NewManager()
			if _, err := m.Connect(tg.cfg); err != nil {
				t.Fatal(err)
			}
			defer m.Disconnect(tg.cfg.ID)
			if res, err := m.Execute(ExecRequest{TabID: "p", ConnID: tg.cfg.ID, Schema: tg.schema, SQL: tg.setup, Cursor: -1}); err != nil || res.Results[len(res.Results)-1].Error != "" {
				t.Fatal(err, res.Results)
			}
			tbl := "items"
			if name == "mssql" {
				tbl = "dbo.items"
			}
			q := "SELECT id, name FROM " + tbl + " ORDER BY id -- trailing comment"
			res, err := m.Execute(ExecRequest{TabID: "p", ConnID: tg.cfg.ID, SQL: q, Cursor: -1, Fetch: 2})
			if err != nil || res.Results[0].Error != "" {
				t.Fatal(err, res.Results)
			}
			r := res.Results[0]
			if !r.Paged || len(r.Rows) != 2 || !r.HasMore || r.CursorID != "" {
				t.Fatalf("first page: %+v", r)
			}
			f, err := m.FetchPage("p", tg.cfg.ID, r.SQL, 2, 2)
			if err != nil || len(f.Rows) != 1 || f.HasMore || f.Rows[0][0] != "3" {
				t.Fatalf("second page: %+v %v", f, err)
			}
			if n, err := m.CountResult("p", tg.cfg.ID, r.SQL); err != nil || n != 3 {
				t.Fatalf("count: %d %v", n, err)
			}
			// exactly one page: no "more"
			res, _ = m.Execute(ExecRequest{TabID: "p", ConnID: tg.cfg.ID, SQL: q, Cursor: -1, Fetch: 3})
			if r := res.Results[0]; !r.Paged || len(r.Rows) != 3 || r.HasMore {
				t.Fatalf("one page: %+v", r)
			}
			// a statement that already limits itself streams, and an idle
			// cursor is closed
			if name == "oracle" || name == "mssql" {
				return
			}
			old := cursorIdle
			cursorIdle = 300 * time.Millisecond
			defer func() { cursorIdle = old }()
			res, _ = m.Execute(ExecRequest{TabID: "p", ConnID: tg.cfg.ID, SQL: "SELECT id FROM items ORDER BY id LIMIT 3", Cursor: -1, Fetch: 1})
			r = res.Results[0]
			if r.Paged || !r.HasMore || r.CursorID == "" {
				t.Fatalf("streaming: %+v", r)
			}
			time.Sleep(time.Second)
			if _, err := m.FetchMore("p", r.CursorID, 1); err == nil || !strings.Contains(err.Error(), "닫았습니다") {
				t.Fatalf("idle cursor should be closed: %v", err)
			}
		})
	}
}

func TestRoutines(t *testing.T) {
	create := map[string][]string{
		"mariadb": {"DROP PROCEDURE IF EXISTS tb_proc; DROP FUNCTION IF EXISTS tb_func",
			"DELIMITER $$\nCREATE PROCEDURE tb_proc(IN n INT) BEGIN SELECT n; SELECT n + 1; END$$\nCREATE FUNCTION tb_func(x INT) RETURNS INT DETERMINISTIC BEGIN DECLARE y INT; SET y = x * 2; RETURN y; END$$\nDELIMITER ;"},
		"postgres": {"DROP PROCEDURE IF EXISTS tb_proc(int); DROP FUNCTION IF EXISTS tb_func(int)",
			"CREATE PROCEDURE tb_proc(n int) LANGUAGE plpgsql AS $$ BEGIN RAISE NOTICE '%', n; END $$;\nCREATE FUNCTION tb_func(x int) RETURNS int LANGUAGE sql AS $$ SELECT x * 2; $$;"},
		"mssql": {"IF OBJECT_ID('dbo.tb_proc') IS NOT NULL DROP PROCEDURE dbo.tb_proc;\nIF OBJECT_ID('dbo.tb_func') IS NOT NULL DROP FUNCTION dbo.tb_func",
			"CREATE PROCEDURE dbo.tb_proc @n int AS BEGIN SELECT @n; SELECT @n + 1; END\nGO\nCREATE FUNCTION dbo.tb_func(@x int) RETURNS int AS BEGIN RETURN @x * 2; END\nGO"},
		"oracle": {"BEGIN EXECUTE IMMEDIATE 'DROP PROCEDURE tb_proc'; EXCEPTION WHEN OTHERS THEN NULL; END;\n/\nBEGIN EXECUTE IMMEDIATE 'DROP FUNCTION tb_func'; EXCEPTION WHEN OTHERS THEN NULL; END;\n/",
			"CREATE OR REPLACE PROCEDURE tb_proc(n NUMBER) AS v NUMBER; BEGIN v := n; v := v + 1; END;\n/\nCREATE OR REPLACE FUNCTION tb_func(x NUMBER) RETURN NUMBER AS BEGIN RETURN x * 2; END;\n/"},
	}
	all := targets(t)
	for name, sqls := range create {
		tg := all[name]
		t.Run(name, func(t *testing.T) {
			m := NewManager()
			if _, err := m.Connect(tg.cfg); err != nil {
				t.Fatal(err)
			}
			defer m.Disconnect(tg.cfg.ID)
			run := func(q string) {
				t.Helper()
				res, err := m.Execute(ExecRequest{TabID: "r", ConnID: tg.cfg.ID, Schema: tg.schema, SQL: q, Cursor: -1})
				if err != nil {
					t.Fatal(err)
				}
				for _, r := range res.Results {
					if r.Error != "" {
						t.Fatalf("%s\n-> %s", r.SQL, r.Error)
					}
				}
			}
			run(sqls[0])
			run(sqls[1])
			list, err := m.Routines(tg.cfg.ID, tg.schema)
			if err != nil {
				t.Fatal(err)
			}
			found := map[string]Routine{}
			for _, r := range list {
				found[strings.ToLower(r.Name)] = r
			}
			p, f := found["tb_proc"], found["tb_func"]
			if p.Type != "PROCEDURE" || f.Type != "FUNCTION" {
				t.Fatalf("routines: %+v", list)
			}
			var sources []string
			for _, r := range []Routine{p, f} {
				src, err := m.RoutineSource(tg.cfg.ID, tg.schema, r)
				if err != nil || !strings.Contains(strings.ToLower(src), "tb_") {
					t.Fatalf("source %s: %v\n%s", r.Name, err, src)
				}
				sources = append(sources, src)
			}
			t.Logf("procedure source:\n%s", sources[0])
			// the source runs again as written (drop first)
			run(sqls[0])
			for _, src := range sources {
				run(src)
			}
			if l2, _ := m.Routines(tg.cfg.ID, tg.schema); len(l2) < 2 {
				t.Fatalf("recreated: %+v", l2)
			}
			run(sqls[0])
		})
	}
}

func TestAlterColumns(t *testing.T) {
	setup := map[string]string{
		"mariadb":  "DROP TABLE IF EXISTS alt_t; CREATE TABLE alt_t (id INT AUTO_INCREMENT PRIMARY KEY, a VARCHAR(20) NULL DEFAULT 'x' COMMENT 'c1', b INT NOT NULL); INSERT INTO alt_t (a, b) VALUES ('v', 1)",
		"postgres": "DROP TABLE IF EXISTS alt_t; CREATE TABLE alt_t (id int PRIMARY KEY, a varchar(20) DEFAULT 'x', b int NOT NULL); COMMENT ON COLUMN alt_t.a IS 'c1'; INSERT INTO alt_t VALUES (1, 'v', 1)",
		"mssql":    "IF OBJECT_ID('dbo.alt_t') IS NOT NULL DROP TABLE dbo.alt_t;\nCREATE TABLE dbo.alt_t (id int PRIMARY KEY, a varchar(20) NULL DEFAULT 'x', b int NOT NULL);\nEXEC sp_addextendedproperty 'MS_Description', 'c1', 'SCHEMA', 'dbo', 'TABLE', 'alt_t', 'COLUMN', 'a';\nINSERT INTO dbo.alt_t VALUES (1, 'v', 1)",
		"oracle":   "BEGIN EXECUTE IMMEDIATE 'DROP TABLE alt_t'; EXCEPTION WHEN OTHERS THEN NULL; END;\n/\nCREATE TABLE alt_t (id NUMBER PRIMARY KEY, a VARCHAR2(20) DEFAULT 'x', b NUMBER NOT NULL);\nCOMMENT ON COLUMN alt_t.a IS 'c1';\nINSERT INTO alt_t VALUES (1, 'v', 1);\nCOMMIT",
	}
	newType := map[string][2]string{ // a, b
		"mariadb": {"varchar(40)", "bigint(20)"}, "postgres": {"varchar(40)", "bigint"},
		"mssql": {"varchar(40)", "bigint"}, "oracle": {"VARCHAR2(40)", "NUMBER"},
	}
	all := targets(t)
	for name, q := range setup {
		tg := all[name]
		t.Run(name, func(t *testing.T) {
			m := NewManager()
			if _, err := m.Connect(tg.cfg); err != nil {
				t.Fatal(err)
			}
			defer m.Disconnect(tg.cfg.ID)
			res, err := m.Execute(ExecRequest{TabID: "a", ConnID: tg.cfg.ID, Schema: tg.schema, SQL: q, Cursor: -1})
			if err != nil || res.Results[len(res.Results)-1].Error != "" {
				t.Fatal(err, res.Results)
			}
			defer m.Execute(ExecRequest{TabID: "a", ConnID: tg.cfg.ID, Schema: tg.schema, SQL: "DROP TABLE alt_t", Cursor: -1})
			tbl := Table{Name: "alt_t", Type: "TABLE"}
			if name == "oracle" {
				tbl.Name = "ALT_T"
			}
			if name == "mssql" {
				tbl.Schema = "dbo"
			}
			cols := func() map[string]Column {
				cs, err := m.Columns(tg.cfg.ID, tg.schema, tbl)
				if err != nil {
					t.Fatal(err)
				}
				out := map[string]Column{}
				for _, c := range cs {
					out[strings.ToLower(c.Name)] = c
				}
				return out
			}
			apply := func(changes []ColumnAlter) {
				t.Helper()
				req := AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: tbl, Changes: changes}
				prev, err := m.PreviewAlter(req)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("%s", strings.Join(prev, ";\n"))
				r, err := m.ApplyAlter(req)
				if err != nil || r.Error != "" || r.Applied != len(prev) {
					t.Fatalf("apply: %+v %v", r, err)
				}
			}
			c := cols()
			a, b := c["a"], c["b"]
			if !strings.Contains(a.Default, "x") || a.Comment != "c1" {
				t.Fatalf("setup: %+v", a)
			}
			na, nb := a, b
			na.Type, na.Nullable, na.Default, na.Comment = newType[name][0], false, "'y'", "c2"
			nb.Type, nb.Nullable, nb.Default, nb.Comment = newType[name][1], true, "5", "cb"
			apply([]ColumnAlter{{Old: a, New: na}, {Old: b, New: nb}})
			c = cols()
			a2, b2 := c["a"], c["b"]
			if !strings.Contains(a2.Type, "40") || a2.Nullable || !strings.Contains(a2.Default, "y") || a2.Comment != "c2" {
				t.Errorf("a after: %+v", a2)
			}
			if !b2.Nullable || !strings.Contains(b2.Default, "5") || b2.Comment != "cb" {
				t.Errorf("b after: %+v", b2)
			}
			if name == "mariadb" && !strings.Contains(strings.ToLower(c["id"].Extra), "auto_increment") {
				t.Errorf("auto_increment lost: %+v", c["id"])
			}
			// remove default and comment
			na2, nb2 := a2, b2
			na2.Default, na2.Comment = "", ""
			nb2.Default, nb2.Comment = "", ""
			apply([]ColumnAlter{{Old: a2, New: na2}, {Old: b2, New: nb2}})
			c = cols()
			if defaultOf(c["a"].Default) != "" || c["a"].Comment != "" || defaultOf(c["b"].Default) != "" || c["b"].Comment != "" {
				t.Errorf("after removal: a=%+v b=%+v", c["a"], c["b"])
			}
			// add a column: NOT NULL with a default fills the existing row
			addReq := AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: tbl,
				Add: []Column{{Name: "new_z", Type: newType[name][0], Default: "'q'", Comment: "cz"}, {Name: "new_n", Type: newType[name][1], Nullable: true}}}
			prev, err := m.PreviewAlter(addReq)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s", strings.Join(prev, ";\n"))
			if r, err := m.ApplyAlter(addReq); err != nil || r.Error != "" || r.Applied != len(prev) {
				t.Fatalf("add: %+v %v", r, err)
			}
			c = cols()
			if z := c["new_z"]; z.Nullable || !strings.Contains(z.Default, "q") || z.Comment != "cz" || !strings.Contains(z.Type, "40") {
				t.Errorf("new_z: %+v", z)
			}
			if n, ok := c["new_n"]; !ok || !n.Nullable {
				t.Errorf("new_n: %+v", n)
			}
			// drop a column (new_z has a default: a constraint on SQL Server)
			// and set the table comment, twice (add, then change)
			for _, tc := range []string{"tc1", "tc2"} {
				cur := tbl
				if ts, _ := m.Tables(tg.cfg.ID, tg.schema); true {
					for _, x := range ts {
						if strings.EqualFold(x.Name, tbl.Name) {
							cur.Comment = x.Comment
						}
					}
				}
				req := AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: cur, TableComment: &tc}
				if tc == "tc1" {
					req.Drop = []string{c["new_z"].Name}
				}
				prev, _ := m.PreviewAlter(req)
				t.Logf("%s", strings.Join(prev, ";\n"))
				if r, err := m.ApplyAlter(req); err != nil || r.Error != "" {
					t.Fatalf("drop/comment: %+v %v", r, err)
				}
			}
			c = cols()
			if _, ok := c["new_z"]; ok {
				t.Errorf("new_z not dropped")
			}
			ts, _ := m.Tables(tg.cfg.ID, tg.schema)
			for _, x := range ts {
				if strings.EqualFold(x.Name, tbl.Name) && x.Comment != "tc2" {
					t.Errorf("table comment: %q", x.Comment)
				}
			}
			{ // comment and rename together, then the name back
				cur, tc := tbl, "tc3"
				cur.Comment = "tc2"
				to := "alt_r"
				if name == "oracle" {
					to = "ALT_R"
				}
				req := AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: cur, TableComment: &tc, Rename: to}
				prev, _ := m.PreviewAlter(req)
				t.Logf("%s", strings.Join(prev, ";\n"))
				if r, err := m.ApplyAlter(req); err != nil || r.Error != "" {
					t.Fatalf("rename: %+v %v", r, err)
				}
				found := false
				ts, _ := m.Tables(tg.cfg.ID, tg.schema)
				for _, x := range ts {
					if x.Name == to {
						found = x.Comment == "tc3"
					}
				}
				if !found {
					t.Errorf("renamed table with its comment not found")
				}
				back := cur
				back.Name = to
				if r, err := m.ApplyAlter(AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: back, Rename: tbl.Name}); err != nil || r.Error != "" {
					t.Fatalf("rename back: %+v %v", r, err)
				}
			}
			{ // rename a column with a comment change; then back
				up := func(x string) string {
					if name == "oracle" {
						return strings.ToUpper(x)
					}
					return x
				}
				c = cols()
				old := c["b"]
				nw := old
				nw.Name, nw.Comment = "b_renamed", "rn"
				req := AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: tbl, Changes: []ColumnAlter{{Old: old, New: nw}}}
				prev, _ := m.PreviewAlter(req)
				t.Logf("%s", strings.Join(prev, ";\n"))
				if r, err := m.ApplyAlter(req); err != nil || r.Error != "" {
					t.Fatalf("rename column: %+v %v", r, err)
				}
				c = cols()
				if x, ok := c["b_renamed"]; !ok || x.Comment != "rn" || x.Name != up("b_renamed") {
					t.Errorf("renamed: %+v", c)
				}
				back := c["b_renamed"]
				nb := back
				nb.Name = old.Name
				if r, err := m.ApplyAlter(AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: tbl, Changes: []ColumnAlter{{Old: back, New: nb}}}); err != nil || r.Error != "" {
					t.Fatalf("rename back: %+v %v", r, err)
				}
				c = cols()
			}
			{ // auto increment: off and on again where an existing column can
				// change (MySQL/MariaDB, PostgreSQL); refused elsewhere. A new
				// identity column everywhere but MySQL (one per table, id has it).
				c = cols()
				id := c["id"]
				flip := func(on bool) ColumnAlter {
					cur := cols()["id"]
					nw := cur
					nw.Extra = ""
					if on {
						nw.Extra = "auto_increment"
					}
					return ColumnAlter{Old: cur, New: nw}
				}
				switch name {
				case "mariadb", "postgres":
					want := name == "postgres" // pg: id starts without identity
					for _, on := range []bool{want, !want} {
						req := AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: tbl, Changes: []ColumnAlter{flip(on)}}
						prev, _ := m.PreviewAlter(req)
						t.Logf("%s", strings.Join(prev, ";\n"))
						if r, err := m.ApplyAlter(req); err != nil || r.Error != "" {
							t.Fatalf("auto %v: %+v %v", on, r, err)
						}
						if got := autoInc(cols()["id"]); got != on {
							t.Errorf("auto: %v, want %v (%+v)", got, on, cols()["id"])
						}
					}
				default:
					if _, err := m.PreviewAlter(AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: tbl, Changes: []ColumnAlter{flip(!autoInc(id))}}); err == nil {
						t.Errorf("auto on an existing column: no error")
					}
				}
				if name != "mariadb" {
					typ := map[string]string{"postgres": "int", "mssql": "int", "oracle": "NUMBER"}[name]
					req := AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: tbl, Add: []Column{{Name: "seq_no", Type: typ, Extra: "auto_increment"}}}
					prev, _ := m.PreviewAlter(req)
					t.Logf("%s", strings.Join(prev, ";\n"))
					if r, err := m.ApplyAlter(req); err != nil || r.Error != "" {
						t.Fatalf("add identity: %+v %v", r, err)
					}
					if name != "oracle" && !autoInc(cols()["seq_no"]) { // (Oracle's list shows no identity)
						t.Errorf("seq_no: %+v", cols()["seq_no"])
					}
					sq := cols()["seq_no"].Name
					if r, err := m.ApplyAlter(AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: tbl, Drop: []string{sq}}); err != nil || r.Error != "" {
						t.Fatalf("drop identity col: %+v %v", r, err)
					}
				}
				c = cols()
			}
			if name == "mariadb" { // engine
				o, err := m.TableOptions(tg.cfg.ID, tg.schema, tbl)
				if err != nil || o.Engine != "InnoDB" || !slices.Contains(o.Engines, "MyISAM") {
					t.Fatalf("options: %+v %v", o, err)
				}
				// collation and ON UPDATE: a new column with both, then each
				// changed on its own (the other kept)
				req := AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: tbl, Add: []Column{
					{Name: "ts", Type: "timestamp", Nullable: true, Default: "CURRENT_TIMESTAMP", OnUpdate: "CURRENT_TIMESTAMP"},
					{Name: "code", Type: "varchar(10)", Nullable: true, Collation: "utf8mb4_bin"}}}
				prev, _ := m.PreviewAlter(req)
				t.Logf("%s", strings.Join(prev, ";\n"))
				if r, err := m.ApplyAlter(req); err != nil || r.Error != "" {
					t.Fatalf("add ts/code: %+v %v", r, err)
				}
				o, _ = m.TableOptions(tg.cfg.ID, tg.schema, tbl)
				if !strings.EqualFold(strings.TrimSuffix(o.Columns["ts"].OnUpdate, "()"), "current_timestamp") || o.Columns["code"].Collation != "utf8mb4_bin" {
					t.Fatalf("options after add: %+v", o.Columns)
				}
				cs := cols()
				ts, code := cs["ts"], cs["code"]
				ts.OnUpdate, code.Collation = o.Columns["ts"].OnUpdate, "utf8mb4_bin"
				nts, ncode := ts, code
				nts.OnUpdate, nts.Comment = "", "no on update" // ON UPDATE off
				ncode.Collation = "utf8mb4_general_ci"
				req = AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: tbl, Changes: []ColumnAlter{{Old: ts, New: nts}, {Old: code, New: ncode}}}
				prev, _ = m.PreviewAlter(req)
				t.Logf("%s", strings.Join(prev, ";\n"))
				if r, err := m.ApplyAlter(req); err != nil || r.Error != "" {
					t.Fatalf("change ts/code: %+v %v", r, err)
				}
				o, _ = m.TableOptions(tg.cfg.ID, tg.schema, tbl)
				if o.Columns["ts"].OnUpdate != "" || o.Columns["code"].Collation != "utf8mb4_general_ci" {
					t.Errorf("options after change: %+v", o.Columns)
				}
				// a plain comment change keeps them
				cs = cols()
				code = cs["code"]
				ncode = code
				ncode.Comment = "kept"
				if r, err := m.ApplyAlter(AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: tbl, Changes: []ColumnAlter{{Old: code, New: ncode}}}); err != nil || r.Error != "" {
					t.Fatalf("comment: %+v %v", r, err)
				}
				if o, _ = m.TableOptions(tg.cfg.ID, tg.schema, tbl); o.Columns["code"].Collation != "utf8mb4_general_ci" {
					t.Errorf("collation lost: %+v", o.Columns["code"])
				}
				if r, err := m.ApplyAlter(AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: tbl, Drop: []string{"ts", "code"}}); err != nil || r.Error != "" {
					t.Fatalf("drop ts/code: %+v %v", r, err)
				}
				for _, e := range []string{"MyISAM", "InnoDB"} {
					if r, err := m.ApplyAlter(AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: tbl, Engine: e}); err != nil || r.Error != "" {
						t.Fatalf("engine %s: %+v %v", e, r, err)
					}
					if o, _ := m.TableOptions(tg.cfg.ID, tg.schema, tbl); o.Engine != e {
						t.Errorf("engine: %q, want %s", o.Engine, e)
					}
				}
			}
			if name == "mariadb" { // reorder, and add in the middle
				cs, _ := m.Columns(tg.cfg.ID, tg.schema, tbl)
				var ch []ColumnAlter
				for _, c := range cs {
					ch = append(ch, ColumnAlter{Old: c, New: c})
				}
				// new_n first, a renamed to a2, mid added after it
				for i := range ch {
					if ch[i].Old.Name == "a" {
						ch[i].New.Name = "a2"
					}
				}
				order := []string{"new_n", "id", "a", "mid", "b"}
				want := []string{"new_n", "id", "a2", "mid", "b"}
				req := AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: tbl, Changes: ch, Order: order,
					Add: []Column{{Name: "mid", Type: "int", Nullable: true}}}
				prev, err := m.PreviewAlter(req)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("%s", strings.Join(prev, ";\n"))
				if r, err := m.ApplyAlter(req); err != nil || r.Error != "" {
					t.Fatalf("reorder: %+v %v", r, err)
				}
				cs, _ = m.Columns(tg.cfg.ID, tg.schema, tbl)
				var got []string
				for _, c := range cs {
					got = append(got, c.Name)
				}
				if strings.Join(got, ",") != strings.Join(want, ",") {
					t.Errorf("order: %v", got)
				}
				c = cols()
				if !strings.Contains(strings.ToLower(c["id"].Extra), "auto_increment") {
					t.Errorf("auto_increment lost: %+v", c["id"])
				}
			}
			// a failing change reports its error
			bad, ok := c["a"]
			if !ok {
				bad = c["a2"] // renamed by the reorder step
			}
			nbad := bad
			nbad.Type = "no_such_type_xyz"
			r, err := m.ApplyAlter(AlterRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: tbl, Changes: []ColumnAlter{{Old: bad, New: nbad}}})
			if err != nil || r.Error == "" || r.Applied != 0 {
				t.Errorf("bad type: %+v %v", r, err)
			}
		})
	}
}

// A materialized view has a container table of the same name: the tree
// must list it once (duplicate keys break rendering).
func TestOracleMviewListedOnce(t *testing.T) {
	tg := targets(t)["oracle"]
	m := NewManager()
	if _, err := m.Connect(tg.cfg); err != nil {
		t.Fatal(err)
	}
	defer m.Disconnect(tg.cfg.ID)
	res, err := m.Execute(ExecRequest{TabID: "mv", ConnID: tg.cfg.ID, SQL: "BEGIN EXECUTE IMMEDIATE 'DROP MATERIALIZED VIEW tb_mv'; EXCEPTION WHEN OTHERS THEN NULL; END;\n/\nCREATE MATERIALIZED VIEW tb_mv AS SELECT 1 x FROM dual", Cursor: -1})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res.Results {
		if r.Error != "" {
			t.Fatalf("%s -> %s", r.SQL, r.Error)
		}
	}
	ts, err := m.Tables(tg.cfg.ID, tg.schema)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, x := range ts {
		if x.Name == "TB_MV" {
			n++
			if x.Type != "MATERIALIZED VIEW" {
				t.Errorf("type %q", x.Type)
			}
		}
	}
	if n != 1 {
		t.Fatalf("TB_MV listed %d times", n)
	}
}

func TestForeignKeys(t *testing.T) {
	setup := map[string]string{
		"mariadb":  "DROP TABLE IF EXISTS fk_c; DROP TABLE IF EXISTS fk_p; CREATE TABLE fk_p (a INT, b INT, PRIMARY KEY (a, b)); CREATE TABLE fk_c (id INT PRIMARY KEY, x INT, y INT)",
		"postgres": "DROP TABLE IF EXISTS fk_c; DROP TABLE IF EXISTS fk_p; CREATE TABLE fk_p (a int, b int, PRIMARY KEY (a, b)); CREATE TABLE fk_c (id int PRIMARY KEY, x int, y int)",
		"mssql":    "IF OBJECT_ID('dbo.fk_c') IS NOT NULL DROP TABLE dbo.fk_c;\nIF OBJECT_ID('dbo.fk_p') IS NOT NULL DROP TABLE dbo.fk_p;\nCREATE TABLE dbo.fk_p (a int NOT NULL, b int NOT NULL, PRIMARY KEY (a, b));\nCREATE TABLE dbo.fk_c (id int PRIMARY KEY, x int, y int)",
		"oracle":   "BEGIN EXECUTE IMMEDIATE 'DROP TABLE fk_c'; EXCEPTION WHEN OTHERS THEN NULL; END;\n/\nBEGIN EXECUTE IMMEDIATE 'DROP TABLE fk_p'; EXCEPTION WHEN OTHERS THEN NULL; END;\n/\nCREATE TABLE fk_p (a NUMBER, b NUMBER, PRIMARY KEY (a, b));\nCREATE TABLE fk_c (id NUMBER PRIMARY KEY, x NUMBER, y NUMBER)",
	}
	all := targets(t)
	for name, q := range setup {
		tg := all[name]
		t.Run(name, func(t *testing.T) {
			m := NewManager()
			if _, err := m.Connect(tg.cfg); err != nil {
				t.Fatal(err)
			}
			defer m.Disconnect(tg.cfg.ID)
			res, err := m.Execute(ExecRequest{TabID: "a", ConnID: tg.cfg.ID, Schema: tg.schema, SQL: q, Cursor: -1})
			if err != nil || res.Results[len(res.Results)-1].Error != "" {
				t.Fatal(err, res.Results)
			}
			defer m.Execute(ExecRequest{TabID: "a", ConnID: tg.cfg.ID, Schema: tg.schema, SQL: "DROP TABLE fk_c;\nDROP TABLE fk_p", Cursor: -1})
			up := func(s string) string {
				if name == "oracle" {
					return strings.ToUpper(s)
				}
				return s
			}
			child, parent := Table{Name: up("fk_c"), Type: "TABLE"}, Table{Name: up("fk_p"), Type: "TABLE"}
			if name == "mssql" {
				child.Schema, parent.Schema = "dbo", "dbo"
			}
			onUpdate := "CASCADE"
			if name == "oracle" {
				onUpdate = ""
			}
			fk := TableFK{Name: up("fk_c_xy"), Columns: []string{up("x"), up("y")}, RefTable: parent, RefColumns: []string{up("a"), up("b")}, OnDelete: "CASCADE", OnUpdate: onUpdate}
			req := FKRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: child, Create: []TableFK{fk}}
			prev, err := m.PreviewForeignKeys(req)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s", strings.Join(prev, ";\n"))
			if r, err := m.ApplyForeignKeys(req); err != nil || r.Error != "" {
				t.Fatalf("add: %+v %v", r, err)
			}
			got, err := m.ListForeignKeys(tg.cfg.ID, tg.schema, child)
			if err != nil || len(got) != 1 {
				t.Fatalf("list: %+v %v", got, err)
			}
			g := got[0]
			if g.Name != fk.Name || strings.Join(g.Columns, ",") != strings.Join(fk.Columns, ",") || strings.Join(g.RefColumns, ",") != strings.Join(fk.RefColumns, ",") ||
				!strings.EqualFold(g.RefTable.Name, parent.Name) || g.RefSchema != "" || g.OnDelete != "CASCADE" || g.OnUpdate != onUpdate && !(name == "oracle" && g.OnUpdate == "") {
				t.Errorf("listed: %+v", g)
			}
			if cs, _ := m.Columns(tg.cfg.ID, tg.schema, child); !strings.Contains(strings.ToLower(cs[1].FK), "fk_p") {
				t.Errorf("column fk: %+v", cs[1])
			}
			req = FKRequest{ConnID: tg.cfg.ID, Schema: tg.schema, Table: child, Drop: got}
			prev, _ = m.PreviewForeignKeys(req)
			t.Logf("%s", strings.Join(prev, ";\n"))
			if r, err := m.ApplyForeignKeys(req); err != nil || r.Error != "" {
				t.Fatalf("drop: %+v %v", r, err)
			}
			if got, _ := m.ListForeignKeys(tg.cfg.ID, tg.schema, child); len(got) != 0 {
				t.Errorf("after drop: %+v", got)
			}
		})
	}
}
