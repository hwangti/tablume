package dbx

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"
	goora "github.com/sijms/go-ora/v2"
	_ "modernc.org/sqlite"
)

// Dialect hides the differences between database engines.
type Dialect interface {
	DriverName() string
	DSN(c ConnConfig) string
	Style() string // statement splitting style, see Split
	ListSchemas(ctx context.Context, q Querier) (SchemaList, error)
	CurrentSchema(ctx context.Context, q Querier) string
	ListTables(ctx context.Context, q Querier, schema string) ([]Table, error)
	ListColumns(ctx context.Context, q Querier, schema string, t Table) ([]Column, error)
	// AllColumns returns table name -> column names, used for autocompletion.
	AllColumns(ctx context.Context, q Querier, schema string) (map[string][]string, error)
	UseSchema(schema string) string
	QualifiedName(schema string, t Table) string
	SelectTop(qualified string, n int) string
	Version(ctx context.Context, q Querier) string
}

type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func DialectFor(driver string) (Dialect, error) {
	switch driver {
	case "mysql", "mariadb":
		return mysqlDialect{}, nil
	case "postgres":
		return pgDialect{}, nil
	case "mssql":
		return mssqlDialect{}, nil
	case "oracle":
		return oracleDialect{}, nil
	case "sqlite":
		return sqliteDialect{}, nil
	}
	return nil, fmt.Errorf("지원하지 않는 드라이버: %s", driver)
}

func parseParams(s string) url.Values {
	v := url.Values{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '&' || r == '\n' || r == ';' }) {
		k, val, _ := strings.Cut(strings.TrimSpace(part), "=")
		if k != "" {
			v.Set(strings.TrimSpace(k), strings.TrimSpace(val))
		}
	}
	return v
}

func scanStrings(ctx context.Context, q Querier, query string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s sql.NullString
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s.String)
	}
	return out, rows.Err()
}

func scanTables(ctx context.Context, q Querier, query string, args ...any) ([]Table, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	out := []Table{}
	for rows.Next() {
		var t Table
		if len(cols) == 3 {
			err = rows.Scan(&t.Schema, &t.Name, &t.Type)
		} else {
			err = rows.Scan(&t.Name, &t.Type)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// scanColumns expects rows of (name, type, nullable, pk) where nullable and
// pk come back as anything truthy ("YES", "Y", 1, true ...).
func scanColumns(ctx context.Context, q Querier, query string, args ...any) ([]Column, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Column{}
	for rows.Next() {
		var name, typ sql.NullString
		var nullable, pk any
		if err := rows.Scan(&name, &typ, &nullable, &pk); err != nil {
			return nil, err
		}
		out = append(out, Column{Name: name.String, Type: typ.String, Nullable: truthy(nullable), PK: truthy(pk)})
	}
	return out, rows.Err()
}

func scanColumnMap(ctx context.Context, q Querier, query string, args ...any) (map[string][]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var t, c sql.NullString
		if err := rows.Scan(&t, &c); err != nil {
			return nil, err
		}
		out[t.String] = append(out[t.String], c.String)
	}
	return out, rows.Err()
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int64:
		return x != 0
	case float64:
		return x != 0
	case []byte:
		return truthy(string(x))
	case string:
		s := strings.ToUpper(strings.TrimSpace(x))
		return s == "YES" || s == "Y" || s == "1" || s == "TRUE" || s == "T"
	}
	if v == nil {
		return false
	}
	s := fmt.Sprint(v)
	return s != "0" && s != "" && s != "false"
}

func quoteWith(ident, open, close string) string {
	return open + strings.ReplaceAll(ident, close, close+close) + close
}

func firstString(ctx context.Context, q Querier, query string) string {
	var s sql.NullString
	if err := q.QueryRowContext(ctx, query).Scan(&s); err != nil {
		return ""
	}
	return s.String
}

// ---------------------------------------------------------------- MySQL / MariaDB

type mysqlDialect struct{}

func (mysqlDialect) DriverName() string { return "mysql" }
func (mysqlDialect) Style() string      { return "mysql" }

func (mysqlDialect) DSN(c ConnConfig) string {
	p := parseParams(c.Params)
	if p.Get("charset") == "" {
		p.Set("charset", "utf8mb4")
	}
	if p.Get("timeout") == "" {
		p.Set("timeout", "10s")
	}
	if p.Get("clientFoundRows") == "" {
		p.Set("clientFoundRows", "true") // report matched rows, like most clients
	}
	port := c.Port
	if port == 0 {
		port = 3306
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s", c.User, c.Password, c.Host, port, c.Database, p.Encode())
}

func (mysqlDialect) ListSchemas(ctx context.Context, q Querier) (SchemaList, error) {
	s, err := scanStrings(ctx, q, "SHOW DATABASES")
	return SchemaList{Schemas: s, Current: firstString(ctx, q, "SELECT DATABASE()")}, err
}

func (mysqlDialect) CurrentSchema(ctx context.Context, q Querier) string {
	return firstString(ctx, q, "SELECT DATABASE()")
}

func (mysqlDialect) ListTables(ctx context.Context, q Querier, schema string) ([]Table, error) {
	return scanTables(ctx, q, `SELECT TABLE_NAME, CASE TABLE_TYPE WHEN 'BASE TABLE' THEN 'TABLE' ELSE TABLE_TYPE END
		FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME`, schema)
}

func (mysqlDialect) ListColumns(ctx context.Context, q Querier, schema string, t Table) ([]Column, error) {
	return scanColumns(ctx, q, `SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY = 'PRI'
		FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`, schema, t.Name)
}

func (mysqlDialect) AllColumns(ctx context.Context, q Querier, schema string) (map[string][]string, error) {
	return scanColumnMap(ctx, q, `SELECT TABLE_NAME, COLUMN_NAME FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME, ORDINAL_POSITION`, schema)
}

func (mysqlDialect) UseSchema(s string) string { return "USE " + quoteWith(s, "`", "`") }
func (mysqlDialect) QualifiedName(schema string, t Table) string {
	return quoteWith(schema, "`", "`") + "." + quoteWith(t.Name, "`", "`")
}
func (mysqlDialect) SelectTop(q string, n int) string {
	return fmt.Sprintf("SELECT * FROM %s LIMIT %d", q, n)
}
func (mysqlDialect) Version(ctx context.Context, q Querier) string {
	return firstString(ctx, q, "SELECT VERSION()")
}

// ---------------------------------------------------------------- PostgreSQL

type pgDialect struct{}

func (pgDialect) DriverName() string { return "pgx" }
func (pgDialect) Style() string      { return "postgres" }

func pgQuoteValue(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

func (pgDialect) DSN(c ConnConfig) string {
	port := c.Port
	if port == 0 {
		port = 5432
	}
	db := c.Database
	if db == "" {
		db = "postgres"
	}
	parts := []string{
		"host=" + pgQuoteValue(c.Host),
		"port=" + strconv.Itoa(port),
		"user=" + pgQuoteValue(c.User),
		"password=" + pgQuoteValue(c.Password),
		"dbname=" + pgQuoteValue(db),
		"connect_timeout=10",
		"application_name=Tablume",
	}
	p := parseParams(c.Params)
	if p.Get("sslmode") == "" {
		p.Set("sslmode", "prefer")
	}
	for k := range p {
		parts = append(parts, k+"="+pgQuoteValue(p.Get(k)))
	}
	return strings.Join(parts, " ")
}

func (pgDialect) ListSchemas(ctx context.Context, q Querier) (SchemaList, error) {
	s, err := scanStrings(ctx, q, `SELECT nspname FROM pg_namespace
		WHERE nspname NOT LIKE 'pg\_toast%' AND nspname NOT LIKE 'pg\_temp%'
		ORDER BY nspname IN ('pg_catalog','information_schema'), nspname`)
	return SchemaList{Schemas: s, Current: firstString(ctx, q, "SELECT current_schema()")}, err
}

func (pgDialect) CurrentSchema(ctx context.Context, q Querier) string {
	return firstString(ctx, q, "SELECT current_schema()")
}

func (pgDialect) ListTables(ctx context.Context, q Querier, schema string) ([]Table, error) {
	return scanTables(ctx, q, `SELECT c.relname,
		CASE c.relkind WHEN 'v' THEN 'VIEW' WHEN 'm' THEN 'MATERIALIZED VIEW' WHEN 'f' THEN 'FOREIGN TABLE' ELSE 'TABLE' END
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN ('r','v','m','p','f') ORDER BY c.relname`, schema)
}

func (pgDialect) ListColumns(ctx context.Context, q Querier, schema string, t Table) ([]Column, error) {
	return scanColumns(ctx, q, `SELECT a.attname, format_type(a.atttypid, a.atttypmod), NOT a.attnotnull,
		EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid = a.attrelid AND i.indisprimary AND a.attnum = ANY(i.indkey))
		FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum`, schema, t.Name)
}

func (pgDialect) AllColumns(ctx context.Context, q Querier, schema string) (map[string][]string, error) {
	return scanColumnMap(ctx, q, `SELECT c.relname, a.attname
		FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN ('r','v','m','p','f') AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY c.relname, a.attnum`, schema)
}

func (pgDialect) UseSchema(s string) string { return "SET search_path TO " + quoteWith(s, `"`, `"`) }
func (pgDialect) QualifiedName(schema string, t Table) string {
	return quoteWith(schema, `"`, `"`) + "." + quoteWith(t.Name, `"`, `"`)
}
func (pgDialect) SelectTop(q string, n int) string {
	return fmt.Sprintf("SELECT * FROM %s LIMIT %d", q, n)
}
func (pgDialect) Version(ctx context.Context, q Querier) string {
	return firstString(ctx, q, "SHOW server_version")
}

// ---------------------------------------------------------------- SQL Server
// The tree node ("schema") is a database; tables carry their own schema.

type mssqlDialect struct{}

func (mssqlDialect) DriverName() string { return "sqlserver" }
func (mssqlDialect) Style() string      { return "mssql" }

func (mssqlDialect) DSN(c ConnConfig) string {
	port := c.Port
	if port == 0 {
		port = 1433
	}
	p := parseParams(c.Params)
	if c.Database != "" {
		p.Set("database", c.Database)
	}
	if p.Get("TrustServerCertificate") == "" {
		p.Set("TrustServerCertificate", "true")
	}
	if p.Get("app name") == "" {
		p.Set("app name", "Tablume")
	}
	u := url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(c.User, c.Password),
		RawQuery: p.Encode(),
	}
	// "host\instance" uses the SQL Browser; otherwise host:port.
	if host, inst, ok := strings.Cut(c.Host, `\`); ok {
		u.Host = host
		u.Path = inst
	} else {
		u.Host = fmt.Sprintf("%s:%d", c.Host, port)
	}
	return u.String()
}

func msq(s string) string { return quoteWith(s, "[", "]") }

func (mssqlDialect) ListSchemas(ctx context.Context, q Querier) (SchemaList, error) {
	s, err := scanStrings(ctx, q, "SELECT name FROM sys.databases WHERE HAS_DBACCESS(name) = 1 ORDER BY database_id")
	return SchemaList{Schemas: s, Current: firstString(ctx, q, "SELECT DB_NAME()")}, err
}

func (mssqlDialect) CurrentSchema(ctx context.Context, q Querier) string {
	return firstString(ctx, q, "SELECT DB_NAME()")
}

func (mssqlDialect) ListTables(ctx context.Context, q Querier, db string) ([]Table, error) {
	return scanTables(ctx, q, `SELECT TABLE_SCHEMA, TABLE_NAME, CASE TABLE_TYPE WHEN 'BASE TABLE' THEN 'TABLE' ELSE TABLE_TYPE END
		FROM `+msq(db)+`.INFORMATION_SCHEMA.TABLES ORDER BY CASE TABLE_SCHEMA WHEN 'dbo' THEN 0 ELSE 1 END, TABLE_SCHEMA, TABLE_NAME`)
}

func (mssqlDialect) ListColumns(ctx context.Context, q Querier, db string, t Table) ([]Column, error) {
	d := msq(db)
	return scanColumns(ctx, q, `SELECT c.COLUMN_NAME,
		c.DATA_TYPE + CASE
			WHEN c.CHARACTER_MAXIMUM_LENGTH = -1 THEN '(max)'
			WHEN c.CHARACTER_MAXIMUM_LENGTH IS NOT NULL THEN '(' + CAST(c.CHARACTER_MAXIMUM_LENGTH AS varchar(10)) + ')'
			WHEN c.DATA_TYPE IN ('decimal','numeric') THEN '(' + CAST(c.NUMERIC_PRECISION AS varchar(10)) + ',' + CAST(c.NUMERIC_SCALE AS varchar(10)) + ')'
			ELSE '' END,
		c.IS_NULLABLE,
		CASE WHEN EXISTS (SELECT 1 FROM `+d+`.INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
			JOIN `+d+`.INFORMATION_SCHEMA.KEY_COLUMN_USAGE k ON k.CONSTRAINT_NAME = tc.CONSTRAINT_NAME AND k.CONSTRAINT_SCHEMA = tc.CONSTRAINT_SCHEMA
			WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY' AND k.TABLE_SCHEMA = c.TABLE_SCHEMA AND k.TABLE_NAME = c.TABLE_NAME AND k.COLUMN_NAME = c.COLUMN_NAME)
			THEN 1 ELSE 0 END
		FROM `+d+`.INFORMATION_SCHEMA.COLUMNS c WHERE c.TABLE_SCHEMA = @p1 AND c.TABLE_NAME = @p2 ORDER BY c.ORDINAL_POSITION`, t.Schema, t.Name)
}

func (mssqlDialect) AllColumns(ctx context.Context, q Querier, db string) (map[string][]string, error) {
	return scanColumnMap(ctx, q, `SELECT TABLE_NAME, COLUMN_NAME FROM `+msq(db)+`.INFORMATION_SCHEMA.COLUMNS ORDER BY TABLE_NAME, ORDINAL_POSITION`)
}

func (mssqlDialect) UseSchema(s string) string { return "USE " + msq(s) }
func (mssqlDialect) QualifiedName(db string, t Table) string {
	s := t.Schema
	if s == "" {
		s = "dbo"
	}
	return msq(db) + "." + msq(s) + "." + msq(t.Name)
}
func (mssqlDialect) SelectTop(q string, n int) string {
	return fmt.Sprintf("SELECT TOP %d * FROM %s", n, q)
}
func (mssqlDialect) Version(ctx context.Context, q Querier) string {
	return firstString(ctx, q, "SELECT CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128)) + ' ' + CAST(SERVERPROPERTY('Edition') AS nvarchar(128))")
}

// ---------------------------------------------------------------- Oracle
// Uses go-ora, a pure Go implementation: no Instant Client needed.

type oracleDialect struct{}

func (oracleDialect) DriverName() string { return "oracle" }
func (oracleDialect) Style() string      { return "oracle" }

func (oracleDialect) DSN(c ConnConfig) string {
	port := c.Port
	if port == 0 {
		port = 1521
	}
	opts := map[string]string{}
	params := parseParams(c.Params)
	for k := range params {
		opts[k] = params.Get(k)
	}
	service := c.Database
	if c.OracleSID {
		opts["SID"] = c.Database
		service = ""
	}
	if _, ok := opts["CONNECTION TIMEOUT"]; !ok {
		opts["CONNECTION TIMEOUT"] = "10"
	}
	return goora.BuildUrl(c.Host, port, service, c.User, c.Password, opts)
}

func (oracleDialect) ListSchemas(ctx context.Context, q Querier) (SchemaList, error) {
	cur := firstString(ctx, q, "SELECT SYS_CONTEXT('USERENV','CURRENT_SCHEMA') FROM dual")
	// Hide Oracle-maintained accounts (SYS, XDB, ...) on 12c+; fall back to all users on 11g.
	s, err := scanStrings(ctx, q, "SELECT username FROM all_users WHERE oracle_maintained = 'N' OR username = :1 ORDER BY username", cur)
	if err != nil {
		s, err = scanStrings(ctx, q, "SELECT username FROM all_users ORDER BY username")
	}
	return SchemaList{Schemas: s, Current: cur}, err
}

func (oracleDialect) CurrentSchema(ctx context.Context, q Querier) string {
	return firstString(ctx, q, "SELECT SYS_CONTEXT('USERENV','CURRENT_SCHEMA') FROM dual")
}

func (oracleDialect) ListTables(ctx context.Context, q Querier, schema string) ([]Table, error) {
	return scanTables(ctx, q, `SELECT table_name, 'TABLE' FROM all_tables WHERE owner = :1 AND table_name NOT LIKE 'BIN$%'
		UNION ALL SELECT view_name, 'VIEW' FROM all_views WHERE owner = :2
		UNION ALL SELECT mview_name, 'MATERIALIZED VIEW' FROM all_mviews WHERE owner = :3
		ORDER BY 1`, schema, schema, schema)
}

func (oracleDialect) ListColumns(ctx context.Context, q Querier, schema string, t Table) ([]Column, error) {
	return scanColumns(ctx, q, `SELECT c.column_name,
		c.data_type || CASE
			WHEN c.data_type IN ('VARCHAR2','NVARCHAR2','CHAR','NCHAR') THEN '(' || c.char_length || ')'
			WHEN c.data_type = 'RAW' THEN '(' || c.data_length || ')'
			WHEN c.data_type = 'NUMBER' AND c.data_precision IS NOT NULL THEN '(' || c.data_precision || ',' || c.data_scale || ')'
			ELSE '' END,
		c.nullable,
		CASE WHEN EXISTS (SELECT 1 FROM all_constraints k JOIN all_cons_columns kc ON kc.owner = k.owner AND kc.constraint_name = k.constraint_name
			WHERE k.owner = c.owner AND k.table_name = c.table_name AND k.constraint_type = 'P' AND kc.column_name = c.column_name)
			THEN 1 ELSE 0 END
		FROM all_tab_columns c WHERE c.owner = :1 AND c.table_name = :2 ORDER BY c.column_id`, schema, t.Name)
}

func (oracleDialect) AllColumns(ctx context.Context, q Querier, schema string) (map[string][]string, error) {
	return scanColumnMap(ctx, q, `SELECT table_name, column_name FROM all_tab_columns WHERE owner = :1 ORDER BY table_name, column_id`, schema)
}

func (oracleDialect) UseSchema(s string) string {
	return "ALTER SESSION SET CURRENT_SCHEMA = " + quoteWith(s, `"`, `"`)
}
func (oracleDialect) QualifiedName(schema string, t Table) string {
	return quoteWith(schema, `"`, `"`) + "." + quoteWith(t.Name, `"`, `"`)
}
func (oracleDialect) SelectTop(q string, n int) string {
	// ROWNUM works on every version, including 11g.
	return fmt.Sprintf("SELECT * FROM %s WHERE ROWNUM <= %d", q, n)
}
func (oracleDialect) Version(ctx context.Context, q Querier) string {
	return firstString(ctx, q, "SELECT banner FROM v$version WHERE ROWNUM = 1")
}

// ---------------------------------------------------------------- SQLite

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
	return scanTables(ctx, q, `SELECT name, upper(type) FROM `+quoteWith(schema, `"`, `"`)+`.sqlite_master
		WHERE type IN ('table','view') AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
}

func (sqliteDialect) ListColumns(ctx context.Context, q Querier, schema string, t Table) ([]Column, error) {
	return scanColumns(ctx, q, `SELECT name, type, NOT "notnull", pk > 0 FROM pragma_table_info(?, ?)`, t.Name, schema)
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
func (sqliteDialect) SelectTop(q string, n int) string {
	return fmt.Sprintf("SELECT * FROM %s LIMIT %d", q, n)
}
func (sqliteDialect) Version(ctx context.Context, q Querier) string {
	return "SQLite " + firstString(ctx, q, "SELECT sqlite_version()")
}

var plsqlCreateRe = regexp.MustCompile(`(?is)^CREATE\s+(?:OR\s+REPLACE\s+)?(?:(?:NON)?EDITIONABLE\s+)?(PROCEDURE|FUNCTION|PACKAGE\s+BODY|PACKAGE|TRIGGER|TYPE\s+BODY|TYPE)\s+(?:("[^"]+"|[\w$#]+)\s*\.\s*)?("[^"]+"|[\w$#]+)`)

func oracleIdent(s string) string {
	if strings.HasPrefix(s, `"`) {
		return strings.Trim(s, `"`)
	}
	return strings.ToUpper(s)
}

// AfterExec reports PL/SQL compilation errors, which Oracle doesn't raise as
// errors on CREATE (SQL*Plus prints "created with compilation errors").
func (oracleDialect) AfterExec(ctx context.Context, q Querier, stmt string) string {
	m := plsqlCreateRe.FindStringSubmatch(stripLeadingComments(stmt))
	if m == nil {
		return ""
	}
	typ := strings.ToUpper(strings.Join(strings.Fields(m[1]), " "))
	owner := "SYS_CONTEXT('USERENV','CURRENT_SCHEMA')"
	args := []any{oracleIdent(m[3]), typ}
	if m[2] != "" {
		owner = ":3"
		args = append(args, oracleIdent(m[2]))
	}
	rows, err := q.QueryContext(ctx, `SELECT line, position, text FROM all_errors
		WHERE owner = `+owner+` AND name = :1 AND type = :2 AND attribute = 'ERROR' ORDER BY sequence`, args...)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var line, pos int64
		var text string
		if rows.Scan(&line, &pos, &text) == nil {
			fmt.Fprintf(&b, "\n  %d/%d: %s", line, pos, strings.TrimSpace(text))
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return fmt.Sprintf("%s %s이(가) 컴파일 오류와 함께 생성되었습니다:%s", typ, oracleIdent(m[3]), b.String())
}

func stripLeadingComments(s string) string {
	for {
		s = strings.TrimLeft(s, " \t\r\n")
		switch {
		case strings.HasPrefix(s, "--"):
			i := strings.IndexByte(s, '\n')
			if i < 0 {
				return ""
			}
			s = s[i+1:]
		case strings.HasPrefix(s, "/*"):
			i := strings.Index(s, "*/")
			if i < 0 {
				return ""
			}
			s = s[i+2:]
		default:
			return s
		}
	}
}
