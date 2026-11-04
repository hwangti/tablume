package dbx

import (
	"reflect"
	"testing"
)

func sqls(st []Stmt) []string {
	out := []string{}
	for _, s := range st {
		out = append(out, s.SQL)
	}
	return out
}

func TestSplit(t *testing.T) {
	cases := []struct {
		name, style, in string
		want            []string
	}{
		{"basic", "mysql", "select 1; select 2;\n", []string{"select 1", "select 2"}},
		{"quotes", "mysql", "select ';' ; select \"a;b\", `c;d`; select 'it\\'s;'", []string{"select ';'", "select \"a;b\", `c;d`", "select 'it\\'s;'"}},
		{"comments", "postgres", "-- a;b\nselect 1; /* x; */ select 2", []string{"-- a;b\nselect 1", "/* x; */ select 2"}},
		{"only comment", "postgres", "select 1;\n-- trailing", []string{"select 1"}},
		{"dollar", "postgres", "create function f() returns int as $$ begin return 1; end; $$ language plpgsql; select 1", []string{"create function f() returns int as $$ begin return 1; end; $$ language plpgsql", "select 1"}},
		{"pg begin txn", "postgres", "begin; update t set a=1; commit;", []string{"begin", "update t set a=1", "commit"}},
		{"mysql proc", "mysql", "CREATE PROCEDURE p() BEGIN SELECT 1; IF 1 THEN SELECT 2; END IF; END; SELECT 3", []string{"CREATE PROCEDURE p() BEGIN SELECT 1; IF 1 THEN SELECT 2; END IF; END", "SELECT 3"}},
		{"mysql txn", "mysql", "BEGIN; SELECT CASE WHEN 1 THEN 2 END; COMMIT", []string{"BEGIN", "SELECT CASE WHEN 1 THEN 2 END", "COMMIT"}},
		{"mysql delimiter", "mysql", "DELIMITER $$\nCREATE TRIGGER x BEFORE INSERT ON t FOR EACH ROW BEGIN SET NEW.a=1; END$$\nDELIMITER ;\nSELECT 1;", []string{"CREATE TRIGGER x BEFORE INSERT ON t FOR EACH ROW BEGIN SET NEW.a=1; END", "SELECT 1"}},
		{"oracle plsql", "oracle", "select 1 from dual;\nbegin\n  null;\n  dbms_output.put_line('a;');\nend;\n/\nselect 2 from dual", []string{"select 1 from dual", "begin\n  null;\n  dbms_output.put_line('a;');\nend;", "select 2 from dual"}},
		{"oracle create", "oracle", "CREATE OR REPLACE PROCEDURE p AS\nBEGIN\n  NULL;\nEND;\n/\nCREATE TABLE t (a NUMBER);", []string{"CREATE OR REPLACE PROCEDURE p AS\nBEGIN\n  NULL;\nEND;", "CREATE TABLE t (a NUMBER)"}},
		{"oracle q-quote", "oracle", "select q'[a;b]' from dual; select 1 from dual", []string{"select q'[a;b]' from dual", "select 1 from dual"}},
		{"mssql go", "mssql", "CREATE PROCEDURE p AS\nSELECT 1;\nSELECT 2;\nGO\nSELECT [a;b] FROM t; SELECT 3", []string{"CREATE PROCEDURE p AS\nSELECT 1;\nSELECT 2;", "SELECT [a;b] FROM t", "SELECT 3"}},
		{"mssql try", "mssql", "BEGIN TRY SELECT 1; END TRY BEGIN CATCH SELECT 2; END CATCH; SELECT 3", []string{"BEGIN TRY SELECT 1; END TRY BEGIN CATCH SELECT 2; END CATCH", "SELECT 3"}},
		{"mssql tran", "mssql", "BEGIN TRAN; UPDATE t SET a=1; COMMIT", []string{"BEGIN TRAN", "UPDATE t SET a=1", "COMMIT"}},
		{"sqlite trigger", "sqlite", "CREATE TRIGGER tr AFTER INSERT ON t BEGIN UPDATE t SET a=1; END; BEGIN IMMEDIATE; SELECT 1", []string{"CREATE TRIGGER tr AFTER INSERT ON t BEGIN UPDATE t SET a=1; END", "BEGIN IMMEDIATE", "SELECT 1"}},
		{"korean", "mysql", "select '한글;' as 이름; select 2", []string{"select '한글;' as 이름", "select 2"}},
	}
	for _, c := range cases {
		got := sqls(Split(c.in, c.style))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got  %q\n want %q", c.name, got, c.want)
		}
	}
}

func TestPickAt(t *testing.T) {
	text := "select 1;\nselect 2;  \n\nselect 3"
	st := Split(text, "mysql")
	check := func(pos int, want string) {
		t.Helper()
		got, _ := PickAt(text, st, pos)
		if got.SQL != want {
			t.Errorf("pos %d: got %q want %q", pos, got.SQL, want)
		}
	}
	check(0, "select 1")
	check(9, "select 1")  // right after ';'
	check(10, "select 2") // start of line 2
	check(20, "select 2") // trailing spaces on line 2
	check(21, "select 2") // end of line 2
	check(22, "select 3") // blank line
	check(len(text), "select 3")
}

func TestUTF16(t *testing.T) {
	s := "가😀b"
	if utf16Len(s) != 4 {
		t.Fatal(utf16Len(s))
	}
	if byteOffset(s, 3) != len("가😀") {
		t.Fatal(byteOffset(s, 3))
	}
}

func TestSortTables(t *testing.T) {
	ts := []Table{{Name: "b"}, {Name: "_system"}, {Name: "A"}, {Name: "a_b"}, {Name: "ab"}}
	sortTables(ts)
	var got []string
	for _, x := range ts {
		got = append(got, x.Name)
	}
	if !reflect.DeepEqual(got, []string{"_system", "A", "a_b", "ab", "b"}) {
		t.Fatal(got)
	}
}
