package dbx

import (
	"strings"
	"testing"
)

func TestPagedSQL(t *testing.T) {
	cases := []struct {
		stmt, style string
		n, off      int
		want        string // "" = not pageable
	}{
		{"SELECT * FROM t", "mysql", 501, 0, "SELECT * FROM t\nLIMIT 501"},
		{"select a from t where x = 'limit' -- note;", "postgres", 11, 20, "select a from t where x = 'limit' -- note\nLIMIT 11 OFFSET 20"},
		{"WITH c AS (SELECT 1 a) SELECT * FROM c ORDER BY a", "sqlite", 5, 5, "WITH c AS (SELECT 1 a) SELECT * FROM c ORDER BY a\nLIMIT 5 OFFSET 5"},
		{"SELECT * FROM t ORDER BY id", "mssql", 10, 0, "SELECT * FROM t ORDER BY id\nOFFSET 0 ROWS FETCH NEXT 10 ROWS ONLY"},
		{"SELECT * FROM t", "mssql", 10, 30, "SELECT * FROM t\nORDER BY (SELECT NULL)\nOFFSET 30 ROWS FETCH NEXT 10 ROWS ONLY"},
		{"SELECT DISTINCT a FROM t", "mssql", 10, 0, "SELECT DISTINCT a FROM t\nORDER BY 1\nOFFSET 0 ROWS FETCH NEXT 10 ROWS ONLY"},
		{"SELECT * FROM t", "oracle", 10, 0, "SELECT * FROM t\nFETCH FIRST 10 ROWS ONLY"},
		{"SELECT * FROM t", "oracle", 10, 10, "SELECT * FROM t\nOFFSET 10 ROWS FETCH NEXT 10 ROWS ONLY"},
		{"SELECT * FROM t WHERE id IN (SELECT id FROM u LIMIT 3)", "mysql", 5, 0, "SELECT * FROM t WHERE id IN (SELECT id FROM u LIMIT 3)\nLIMIT 5"},
		// not pageable
		{"SELECT * FROM t LIMIT 10", "mysql", 5, 0, ""},
		{"SELECT TOP 10 * FROM t", "mssql", 5, 0, ""},
		{"SELECT * FROM t FOR UPDATE", "postgres", 5, 0, ""},
		{"SELECT * INTO x FROM t", "mssql", 5, 0, ""},
		{"WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d", "postgres", 5, 0, ""},
		{"SHOW TABLES", "mysql", 5, 0, ""},
		{"SELECT * FROM t FETCH FIRST 5 ROWS ONLY", "oracle", 5, 0, ""},
	}
	for _, c := range cases {
		got, ok := pagedSQL(c.stmt, c.style, c.n, c.off, false)
		if !ok {
			got = ""
		}
		if got != c.want {
			t.Errorf("%s %q:\n got %q\nwant %q", c.style, c.stmt, got, c.want)
		}
	}
	if _, ok := pagedSQL("SELECT * FROM t", "oracle", 5, 0, true); ok {
		t.Error("11g must not be paged")
	}
}

func TestCountSQL(t *testing.T) {
	got := countSQL("SELECT a FROM t WHERE b = 1 ORDER BY a DESC;", "mssql")
	if !strings.Contains(got, "WHERE b = 1 \n) tablume_count") || strings.Contains(got, "ORDER") {
		t.Errorf("%q", got)
	}
	if got := countSQL("SELECT ROW_NUMBER() OVER (ORDER BY a) FROM t", "postgres"); !strings.Contains(got, "OVER (ORDER BY a) FROM t\n)") {
		t.Errorf("window ORDER BY must stay: %q", got)
	}
}
