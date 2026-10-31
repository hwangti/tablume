package dbx

import (
	"os"
	"path/filepath"
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
			if !strings.HasPrefix(r.SQL, "SELECT *") || len(r.Rows) != 2 || !r.HasMore || r.CursorID == "" {
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
			more, err := m.FetchMore("t1", r.CursorID, 10)
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
			q, _ := m.SelectSQL(tg.cfg.ID, tg.schema, *found, 100)
			res, err = m.Execute(ExecRequest{TabID: "t2", ConnID: tg.cfg.ID, SQL: q, Cursor: -1})
			if err != nil || res.Results[0].Error != "" || len(res.Results[0].Rows) != 3 {
				t.Fatalf("select top %q: %v %+v", q, err, res.Results)
			}
		})
	}
}

func TestCursorAndCancel(t *testing.T) {
	all := targets(t)
	cases := map[string][2]string{
		"mariadb":  {"SELECT * FROM seq_1_to_50000000", "SELECT SLEEP(20)"},
		"postgres": {"SELECT * FROM generate_series(1, 50000000)", "SELECT pg_sleep(20)"},
		"mssql":    {"SELECT a.object_id FROM sys.all_objects a CROSS JOIN sys.all_objects b CROSS JOIN sys.all_objects c", "WAITFOR DELAY '00:00:20'"},
		"oracle":   {"SELECT level FROM dual CONNECT BY level <= 50000000", "BEGIN DBMS_SESSION.SLEEP(20); END;"},
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
			if err != nil || res.Results[0].Error != "" || !res.Results[0].HasMore {
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
					if x.Comment != "상품 테이블" || x.Rows < 0 {
						t.Errorf("table meta: %+v", x)
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
