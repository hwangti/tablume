//go:build !no_sqlite

package dbx

// SQLite: connection string, metadata, DDL, column changes, routines
// and bind parameters. Left out of builds with the no_sqlite tag.

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"tablume/internal/i18n"

	"modernc.org/sqlite"
)

func init() {
	register(sqliteDialect{}, "sqlite")
	sqlite.RegisterConnectionHook(func(c sqlite.ExecQuerierContext, _ string) error {
		if !SQLiteSandbox {
			return nil
		}
		a, ok := c.(sqlite.AuthorizerRegisterer)
		if !ok {
			return i18n.Error("SQLite 접근 제한을 걸 수 없습니다")
		}
		return a.RegisterAuthorizer(sandboxAuth)
	})
}

// sandboxAuth refuses what reads or writes other files: ATTACH (VACUUM INTO
// comes as an ATTACH of its target too) and the pragmas that move temporary
// files. load_extension is off in the driver already.
func sandboxAuth(action sqlite.AuthorizerActionCode, arg1, _, _, _ string) sqlite.AuthorizerReturnCode {
	switch action {
	case sqlite.AuthAttach:
		if arg1 == "" || arg1 == ":memory:" {
			return sqlite.AuthorizerOK
		}
		return sqlite.AuthorizerDeny
	case sqlite.AuthPragma:
		if SQLiteDirPragma(arg1) {
			return sqlite.AuthorizerDeny
		}
	}
	return sqlite.AuthorizerOK
}

// SQLiteDirPragma reports a pragma that points SQLite at another directory.
func SQLiteDirPragma(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "temp_store_directory" || n == "data_store_directory"
}

// sqlitePath escapes what a file: URI would read as more than the path:
// ? and # end it and %XX is decoded (%2e%2e would be ..).
var sqlitePath = strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23")

type sqliteDialect struct{}

func (sqliteDialect) DriverName() string { return "sqlite" }
func (sqliteDialect) Style() string      { return "sqlite" }

func (sqliteDialect) DSN(c ConnConfig) string {
	p := parseParams(c.Params)
	if c.ReadOnly {
		p.Add("_pragma", "query_only(1)")
	}
	// The driver takes anything after ? as options, file: URI or not.
	if len(p) == 0 && !strings.ContainsAny(c.Database, "?") {
		return c.Database
	}
	return "file:" + sqlitePath.Replace(c.Database) + "?" + p.Encode()
}

func (sqliteDialect) ListSchemas(ctx context.Context, q Querier) (SchemaList, error) {
	s, err := scanStrings(ctx, q, "SELECT name FROM pragma_database_list WHERE name <> 'temp' ORDER BY seq")
	return SchemaList{Schemas: s, Current: "main"}, err
}

func (sqliteDialect) CurrentSchema(context.Context, Querier) string { return "main" }

func (sqliteDialect) ListTables(ctx context.Context, q Querier, schema string) ([]Table, error) {
	return scanTables(ctx, q, `SELECT name, upper(type), '', -1, -1 FROM `+quoteWith(schema, `"`, `"`)+`.sqlite_master
		WHERE type IN ('table','view') AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
}

func (sqliteDialect) ListColumns(ctx context.Context, q Querier, schema string, t Table) ([]Column, error) {
	return scanColumns(ctx, q, `SELECT p.name, p.type, NOT p."notnull", p.pk > 0, '',
		EXISTS (SELECT 1 FROM pragma_index_list(?1, ?2) il, pragma_index_info(il.name, ?2) ii
			WHERE il."unique" = 1 AND il.origin <> 'pk' AND ii.name = p.name),
		(SELECT f."table" || '.' || f."to" FROM pragma_foreign_key_list(?1, ?2) f WHERE f."from" = p.name),
		p.dflt_value
		FROM pragma_table_info(?1, ?2) p`, t.Name, schema)
}

func (sqliteDialect) AllColumns(ctx context.Context, q Querier, schema string) (map[string][]string, error) {
	return scanColumnMap(ctx, q, `SELECT m.name, p.name FROM `+quoteWith(schema, `"`, `"`)+`.sqlite_master m
		JOIN pragma_table_info(m.name, ?) p WHERE m.type IN ('table','view') ORDER BY m.name, p.cid`, schema)
}

func (sqliteDialect) UseSchema(string) string { return "" }
func (sqliteDialect) QualifiedName(schema string, t Table) string {
	if schema == "" || schema == "main" {
		return quoteWith(t.Name, `"`, `"`)
	}
	return quoteWith(schema, `"`, `"`) + "." + quoteWith(t.Name, `"`, `"`)
}
func (sqliteDialect) Select(q, where, orderBy string, n, offset int) string {
	return selectWithLimit(q, where, orderBy, limitClause(n, offset))
}
func (sqliteDialect) Version(ctx context.Context, q Querier) string {
	return "SQLite " + firstString(ctx, q, "SELECT sqlite_version()")
}

func (sqliteDialect) DDL(ctx context.Context, q *sql.Conn, schema string, t Table) (string, error) {
	rows, err := rowStrings(ctx, q, `SELECT type, sql FROM `+quoteWith(schema, `"`, `"`)+`.sqlite_master
		WHERE (name = ? OR tbl_name = ?) AND sql IS NOT NULL`, t.Name, t.Name)
	if err != nil {
		return "", err
	}
	order := map[string]int{"table": 0, "view": 0, "index": 1, "trigger": 2}
	sort.SliceStable(rows, func(i, j int) bool { return order[rows[i][0]] < order[rows[j][0]] })
	var parts []string
	for _, r := range rows {
		parts = append(parts, strings.TrimSpace(r[1])+";")
	}
	return strings.Join(parts, "\n\n"), nil
}

func (sqliteDialect) Param(int, string) string { return "?" }

// ---- indexes

// SQLite DDL runs inside transactions like any other statement.
func (sqliteDialect) transactionalDDL() bool { return true }

func (sqliteDialect) listIndexes(ctx context.Context, q Querier, schema string, t Table) ([]Index, error) {
	if schema == "" {
		schema = "main"
	}
	rows, err := rowStrings(ctx, q, `SELECT il.name, il."unique", il.origin = 'pk', CASE WHEN il.origin IN ('pk', 'u') THEN il.origin ELSE '' END,
		il.partial, COALESCE(ix.name, ''), ix."desc"
		FROM pragma_index_list(?1, ?2) il, pragma_index_xinfo(il.name, ?2) ix
		WHERE ix."key" = 1
		ORDER BY il.origin = 'pk' DESC, il.name, ix.seqno`, t.Name, schema)
	if err != nil {
		return nil, err
	}
	out := groupIndexes(rows, func(r []string) Index {
		ix := Index{Name: r[0], Unique: r[1] == "1", Primary: r[2] == "1"}
		switch r[3] {
		case "pk":
			ix.Constraint = "PRIMARY KEY"
		case "u":
			ix.Constraint = "UNIQUE"
		}
		if r[4] == "1" {
			ix.Where = "…"
		}
		return ix
	}, func(ix *Index, r []string) {
		ix.Columns = append(ix.Columns, colExpr(r[5], r[6] == "1"))
	})
	// partial indexes: the WHERE part is only in the CREATE statement
	for i := range out {
		if out[i].Where == "" {
			continue
		}
		var src string
		_ = q.QueryRowContext(ctx, `SELECT sql FROM `+quoteWith(schema, `"`, `"`)+`.sqlite_master WHERE type = 'index' AND name = ?`, out[i].Name).Scan(&src)
		if k := strings.LastIndex(strings.ToUpper(src), " WHERE "); k >= 0 {
			out[i].Where = strings.TrimSpace(src[k+7:])
		}
	}
	return out, nil
}

func (sqliteDialect) createIndex(schema string, t Table, ix NewIndex) string {
	quote := func(s string) string { return quoteWith(s, `"`, `"`) }
	name := quote(ix.Name)
	if schema != "" && schema != "main" {
		name = quote(schema) + "." + name // the table can't be qualified here
	}
	return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)", uniqueKw(ix.Unique), name, quote(t.Name), indexColumns(ix, quote))
}

func (sqliteDialect) dropIndex(schema string, t Table, ix Index) (string, error) {
	if ix.Constraint != "" {
		return "", i18n.Error("PRIMARY KEY / UNIQUE 제약에 딸린 인덱스는 테이블을 다시 만들어야 지울 수 있습니다")
	}
	name := quoteWith(ix.Name, `"`, `"`)
	if schema != "" && schema != "main" {
		name = quoteWith(schema, `"`, `"`) + "." + name
	}
	return "DROP INDEX " + name, nil
}

func (d sqliteDialect) renameTable(schema string, t Table, name string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME TO %s", d.QualifiedName(schema, t), quoteWith(name, `"`, `"`))
}

// ---- columns (the structure view)
//
// SQLite's ALTER TABLE adds, renames and drops a column, nothing more: a
// type, NULL or default changes only by making the table again and copying
// it over, which isn't done here. SQLite has no comments either.

func (sqliteDialect) alterColumn(_ context.Context, _ Querier, _ string, _ Table, target string, a ColumnAlter) ([]string, error) {
	d := diffOf(a)
	if d.typ || d.null || d.def || d.auto || d.comment || d.opt {
		return nil, i18n.Error("SQLite에서는 컬럼 이름만 바꿀 수 있습니다 (타입 · NULL · 기본값은 테이블을 새로 만들어야 합니다)")
	}
	if !d.name {
		return nil, nil
	}
	return []string{"ALTER TABLE " + target + " RENAME COLUMN " + quoteWith(a.Old.Name, `"`, `"`) + " TO " + quoteWith(a.New.Name, `"`, `"`)}, nil
}

func (d sqliteDialect) addColumn(schema string, t Table, target string, c Column) []string {
	def, _ := d.columnDef(schema, t, target, c)
	return []string{"ALTER TABLE " + target + " ADD COLUMN " + def}
}

// An auto increment is INTEGER PRIMARY KEY AUTOINCREMENT, on the column
// itself (CREATE TABLE leaves out its PRIMARY KEY clause then).
func (sqliteDialect) columnDef(_ string, _ Table, _ string, c Column) (string, []string) {
	def := quoteWith(c.Name, `"`, `"`) + " " + c.Type
	if autoInc(c) {
		return def + " PRIMARY KEY AUTOINCREMENT", nil
	}
	if !c.Nullable {
		def += " NOT NULL"
	}
	if c.Default != "" {
		def += " DEFAULT " + c.Default
	}
	return def, nil
}

func (sqliteDialect) dropColumn(_ context.Context, _ Querier, _ string, _ Table, target, name string) ([]string, error) {
	return []string{"ALTER TABLE " + target + " DROP COLUMN " + quoteWith(name, `"`, `"`)}, nil
}

// (none: refused before it gets here)
func (sqliteDialect) tableComment(string, Table, string, string) []string { return nil }

// sqliteCheck: what SQLite can't do, said before anything runs. New
// columns: no comment; added to a table, no key or auto increment (ADD
// COLUMN takes neither); made with it, an auto increment is the one
// INTEGER primary key.
func sqliteCheck(tableComment string, cols []Column, creating bool) error {
	if strings.TrimSpace(tableComment) != "" {
		return i18n.Error("SQLite에는 코멘트가 없습니다")
	}
	var pk, auto int
	for _, c := range cols {
		if c.Comment != "" {
			return i18n.Error("SQLite에는 코멘트가 없습니다")
		}
		if c.PK {
			pk++
		}
		if autoInc(c) {
			auto++
			if !creating {
				return i18n.Errorf("%s: SQLite에서는 있는 테이블에 자동 증가 컬럼을 더할 수 없습니다", c.Name)
			}
			if !c.PK || !strings.EqualFold(strings.TrimSpace(c.Type), "integer") {
				return i18n.Errorf("%s: SQLite의 자동 증가는 INTEGER 기본 키 컬럼 하나에만 됩니다", c.Name)
			}
		}
		if c.PK && !creating {
			return i18n.Errorf("%s: SQLite에서는 있는 테이블에 기본 키 컬럼을 더할 수 없습니다", c.Name)
		}
	}
	if auto > 0 && pk > 1 {
		return i18n.Error("SQLite의 자동 증가는 INTEGER 기본 키 컬럼 하나에만 됩니다")
	}
	return nil
}
