package dbx

import "testing"

func TestNoWhere(t *testing.T) {
	cases := []struct {
		style, sql string
		want       bool
	}{
		{"mysql", "UPDATE t SET a = 1", true},
		{"mysql", "update t set a = 1 where id = 2", false},
		{"mysql", "DELETE FROM t", true},
		{"mysql", "DELETE FROM t LIMIT 10", true},
		{"mysql", "DELETE FROM t WHERE id IN (SELECT id FROM u WHERE x)", false},
		{"mysql", "UPDATE t SET a = (SELECT b FROM u WHERE u.id = 1)", true}, // the WHERE is the subquery's
		{"mysql", "UPDATE t SET a = 'where'", true},
		{"mysql", "UPDATE t SET a = 1 -- where id = 1", true},
		{"mysql", "UPDATE t SET a = 'it\\'s where' WHERE id = 1", false},
		{"mssql", "DELETE TOP (10) FROM [where]", true},
		{"postgres", "WITH x AS (SELECT id FROM u WHERE a) DELETE FROM t USING x", true},
		{"postgres", "WITH x AS (SELECT 1) UPDATE t SET a = 1 WHERE id = 1", false},
		{"postgres", "UPDATE t SET a = $$where$$", true},
		{"mysql", "SELECT * FROM t", false},
		{"mysql", "INSERT INTO t VALUES (1)", false},
		{"mysql", "-- note\nDELETE FROM t", true},
	}
	for _, c := range cases {
		if got := noWhere(c.sql, c.style); got != c.want {
			t.Errorf("%q: got %v", c.sql, got)
		}
	}
}

func TestReadsOnly(t *testing.T) {
	cases := []struct {
		style, sql string
		want       bool
	}{
		{"mysql", "SELECT * FROM t WHERE a = 'delete'", true},
		{"mysql", "(SELECT 1) UNION (SELECT 2)", true},
		{"mysql", "SHOW TABLES", true},
		{"mysql", "EXPLAIN SELECT 1", true},
		{"postgres", "EXPLAIN ANALYZE DELETE FROM t", false},
		{"postgres", "WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d", false},
		{"mysql", "SELECT * FROM t FOR UPDATE", false},
		{"mssql", "SELECT * INTO t2 FROM t", false},
		{"mysql", "SELECT REPLACE(a, 'x', 'y') FROM t", true},
		{"mysql", "UPDATE t SET a = 1 WHERE id = 1", false},
		{"mysql", "CALL p()", false},
		{"mysql", "SET SESSION TRANSACTION READ WRITE", false},
		{"oracle", "BEGIN NULL; END;", false},
		{"sqlite", "PRAGMA table_info(t)", true},
		{"sqlite", "PRAGMA query_only = 0", false},
		{"sqlite", "PRAGMA query_only(0)", false},
		{"mysql", "-- only a comment", true},
		{"mysql", "USE shop", true},
	}
	for _, c := range cases {
		if got := readsOnly(c.sql, c.style); got != c.want {
			t.Errorf("%q: got %v", c.sql, got)
		}
	}
}
