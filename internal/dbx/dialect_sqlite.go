//go:build !no_sqlite

package dbx

// SQLite: connection string, metadata, DDL, column changes, routines
// and bind parameters. Left out of builds with the no_sqlite tag.

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

func init() { register(sqliteDialect{}, "sqlite") }

type sqliteDialect struct{}

func (sqliteDialect) DriverName() string { return "sqlite" }
func (sqliteDialect) Style() string      { return "sqlite" }

func (sqliteDialect) DSN(c ConnConfig) string {
	p := parseParams(c.Params)
	if len(p) == 0 {
		return c.Database
	}
	return "file:" + c.Database + "?" + p.Encode()
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
