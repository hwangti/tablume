package dbx

import (
	"context"
	"strings"

	"tablume/internal/i18n"
)

// Schema comments: MariaDB (10.5+) keeps one per database, PostgreSQL per
// schema (COMMENT ON SCHEMA), SQL Server as a database's MS_Description
// property (what SSMS shows as its description). MySQL, Oracle and SQLite
// have none.

type schemaCommentDialect interface {
	schemaComments(ctx context.Context, q Querier) (map[string]string, error)
	setSchemaComment(name, comment string) string
}

func commentMap(rows [][]string) map[string]string {
	out := map[string]string{}
	for _, r := range rows {
		if len(r) == 2 && strings.TrimSpace(r[1]) != "" {
			out[r[0]] = r[1]
		}
	}
	return out
}

func (mysqlDialect) schemaComments(ctx context.Context, q Querier) (map[string]string, error) {
	// MySQL has no SCHEMA_COMMENT column: an error, taken as none
	rows, err := rowStrings(ctx, q, "SELECT SCHEMA_NAME, SCHEMA_COMMENT FROM information_schema.SCHEMATA WHERE SCHEMA_COMMENT <> ''")
	return commentMap(rows), err
}

func (mysqlDialect) setSchemaComment(name, comment string) string {
	return "ALTER DATABASE " + quoteWith(name, "`", "`") + " COMMENT " + sqlQuote(comment, true)
}

func (pgDialect) schemaComments(ctx context.Context, q Querier) (map[string]string, error) {
	rows, err := rowStrings(ctx, q, `SELECT nspname, obj_description(oid, 'pg_namespace') FROM pg_namespace
		WHERE obj_description(oid, 'pg_namespace') IS NOT NULL`)
	return commentMap(rows), err
}

func (pgDialect) setSchemaComment(name, comment string) string {
	c := "NULL"
	if comment != "" {
		c = sqlQuote(comment, false)
	}
	return "COMMENT ON SCHEMA " + quoteWith(name, `"`, `"`) + " IS " + c
}

// One query over every database the login can open: each keeps its
// properties in its own catalog.
func (mssqlDialect) schemaComments(ctx context.Context, q Querier) (map[string]string, error) {
	dbs, err := scanStrings(ctx, q, "SELECT name FROM sys.databases WHERE HAS_DBACCESS(name) = 1 AND state = 0")
	if err != nil || len(dbs) == 0 {
		return map[string]string{}, err
	}
	parts := make([]string, len(dbs))
	for i, d := range dbs {
		parts[i] = "SELECT " + nq(d) + ", CAST(value AS nvarchar(4000)) FROM " + msq(d) +
			".sys.extended_properties WHERE class = 0 AND name = N'MS_Description'"
	}
	rows, err := rowStrings(ctx, q, strings.Join(parts, " UNION ALL "))
	return commentMap(rows), err
}

func (mssqlDialect) setSchemaComment(name, comment string) string {
	d := msq(name)
	exists := "IF EXISTS (SELECT 1 FROM " + d + ".sys.extended_properties WHERE class = 0 AND name = N'MS_Description') "
	proc := "EXEC " + d + ".sys."
	if comment == "" {
		return exists + proc + "sp_dropextendedproperty @name = N'MS_Description'"
	}
	v := nq(comment)
	return exists + proc + "sp_updateextendedproperty @name = N'MS_Description', @value = " + v +
		" ELSE " + proc + "sp_addextendedproperty @name = N'MS_Description', @value = " + v
}

// SetSchemaComment sets (or, empty, removes) a schema's comment and
// returns the statement it ran.
func (m *Manager) SetSchemaComment(id, name, comment string) (string, error) {
	p, err := m.pool(id)
	if err != nil {
		return "", err
	}
	sd, ok := p.d.(schemaCommentDialect)
	if !ok {
		return "", i18n.Error("이 DB에서는 스키마 코멘트를 쓸 수 없습니다")
	}
	if err := p.writable(); err != nil {
		return "", err
	}
	st := sd.setSchemaComment(name, strings.TrimSpace(comment))
	ctx, cancel := metaCtx()
	defer cancel()
	_, err = p.db.ExecContext(ctx, st)
	return st, err
}
