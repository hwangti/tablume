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
	// Select builds SELECT * with an optional WHERE / ORDER BY and a row
	// limit; n <= 0 means no limit (rows are then fetched incrementally).
	Select(qualified, where, orderBy string, n int) string
	Version(ctx context.Context, q Querier) string
	// DDL returns CREATE statements; it gets its own connection because
	// Oracle's DBMS_METADATA options are per session.
	DDL(ctx context.Context, conn *sql.Conn, schema string, t Table) (string, error)
	// Param is the n-th (1-based) bind placeholder for a column of type typ.
	Param(n int, typ string) string
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
	// Rows are (name, type, comment, rows, size), optionally preceded by
	// the inner schema (mssql).
	cols, _ := rows.Columns()
	out := []Table{}
	for rows.Next() {
		var schema, name, typ, comment sql.NullString
		var n, size any
		if len(cols) == 6 {
			err = rows.Scan(&schema, &name, &typ, &comment, &n, &size)
		} else {
			err = rows.Scan(&name, &typ, &comment, &n, &size)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, Table{Schema: schema.String, Name: name.String, Type: typ.String,
			Comment: comment.String, Rows: toInt64(n), Size: toInt64(size)})
	}
	return out, rows.Err()
}

func toInt64(v any) int64 {
	switch x := v.(type) {
	case nil:
		return -1
	case int64:
		return x
	case float64:
		return int64(x)
	case []byte:
		return toInt64(string(x))
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return -1
		}
		return int64(f)
	}
	return toInt64(fmt.Sprint(v))
}

// scanColumns expects rows of (name, type, nullable, pk, comment, unique,
// fk) where the flags come back as anything truthy ("YES", "Y", 1, true ...)
// and fk is the referenced "table.column" or NULL.
func scanColumns(ctx context.Context, q Querier, query string, args ...any) ([]Column, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Column{}
	for rows.Next() {
		var name, typ, comment, fk sql.NullString
		var nullable, pk, unique any
		if err := rows.Scan(&name, &typ, &nullable, &pk, &comment, &unique, &fk); err != nil {
			return nil, err
		}
		out = append(out, Column{Name: name.String, Type: typ.String, Nullable: truthy(nullable), PK: truthy(pk),
			Comment: comment.String, Unique: truthy(unique), FK: fk.String})
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
	return scanTables(ctx, q, `SELECT TABLE_NAME, CASE TABLE_TYPE WHEN 'BASE TABLE' THEN 'TABLE' ELSE TABLE_TYPE END,
		CASE WHEN TABLE_TYPE = 'VIEW' THEN '' ELSE TABLE_COMMENT END,
		TABLE_ROWS, DATA_LENGTH + INDEX_LENGTH
		FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME`, schema)
}

func (mysqlDialect) ListColumns(ctx context.Context, q Querier, schema string, t Table) ([]Column, error) {
	return scanColumns(ctx, q, `SELECT c.COLUMN_NAME, c.COLUMN_TYPE, c.IS_NULLABLE, c.COLUMN_KEY = 'PRI', c.COLUMN_COMMENT,
		EXISTS (SELECT 1 FROM information_schema.STATISTICS s WHERE s.TABLE_SCHEMA = c.TABLE_SCHEMA AND s.TABLE_NAME = c.TABLE_NAME
			AND s.COLUMN_NAME = c.COLUMN_NAME AND s.NON_UNIQUE = 0 AND s.INDEX_NAME <> 'PRIMARY'),
		(SELECT CONCAT(k.REFERENCED_TABLE_NAME, '.', k.REFERENCED_COLUMN_NAME) FROM information_schema.KEY_COLUMN_USAGE k
			WHERE k.TABLE_SCHEMA = c.TABLE_SCHEMA AND k.TABLE_NAME = c.TABLE_NAME AND k.COLUMN_NAME = c.COLUMN_NAME
			AND k.REFERENCED_TABLE_NAME IS NOT NULL LIMIT 1)
		FROM information_schema.COLUMNS c WHERE c.TABLE_SCHEMA = ? AND c.TABLE_NAME = ? ORDER BY c.ORDINAL_POSITION`, schema, t.Name)
}

func (mysqlDialect) AllColumns(ctx context.Context, q Querier, schema string) (map[string][]string, error) {
	return scanColumnMap(ctx, q, `SELECT TABLE_NAME, COLUMN_NAME FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME, ORDINAL_POSITION`, schema)
}

func (mysqlDialect) UseSchema(s string) string { return "USE " + quoteWith(s, "`", "`") }
func (mysqlDialect) QualifiedName(schema string, t Table) string {
	return quoteWith(schema, "`", "`") + "." + quoteWith(t.Name, "`", "`")
}
func (mysqlDialect) Select(q, where, orderBy string, n int) string {
	return selectWithLimit(q, where, orderBy, limitClause(n))
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
		CASE c.relkind WHEN 'v' THEN 'VIEW' WHEN 'm' THEN 'MATERIALIZED VIEW' WHEN 'f' THEN 'FOREIGN TABLE' ELSE 'TABLE' END,
		COALESCE(obj_description(c.oid, 'pg_class'), ''),
		CASE WHEN c.relkind IN ('v','f') OR c.reltuples < 0 THEN -1 ELSE c.reltuples::bigint END,
		CASE WHEN c.relkind IN ('r','m','p') THEN pg_total_relation_size(c.oid) ELSE -1 END
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN ('r','v','m','p','f') ORDER BY c.relname`, schema)
}

func (pgDialect) ListColumns(ctx context.Context, q Querier, schema string, t Table) ([]Column, error) {
	return scanColumns(ctx, q, `SELECT a.attname, format_type(a.atttypid, a.atttypmod), NOT a.attnotnull,
		EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid = a.attrelid AND i.indisprimary AND a.attnum = ANY(i.indkey)),
		COALESCE(col_description(a.attrelid, a.attnum), ''),
		EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid = a.attrelid AND i.indisunique AND NOT i.indisprimary AND a.attnum = ANY(i.indkey)),
		(SELECT rc.relname || '.' || ra.attname FROM pg_constraint con
			JOIN pg_class rc ON rc.oid = con.confrelid
			JOIN pg_attribute ra ON ra.attrelid = con.confrelid AND ra.attnum = con.confkey[array_position(con.conkey, a.attnum)]
			WHERE con.conrelid = a.attrelid AND con.contype = 'f' AND a.attnum = ANY(con.conkey) LIMIT 1)
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
func (pgDialect) Select(q, where, orderBy string, n int) string {
	return selectWithLimit(q, where, orderBy, limitClause(n))
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
	d := msq(db)
	return scanTables(ctx, q, `SELECT s.name, o.name, CASE o.type WHEN 'U' THEN 'TABLE' ELSE 'VIEW' END,
		COALESCE((SELECT CAST(ep.value AS nvarchar(4000)) FROM `+d+`.sys.extended_properties ep
			WHERE ep.class = 1 AND ep.major_id = o.object_id AND ep.minor_id = 0 AND ep.name = 'MS_Description'), ''),
		CASE o.type WHEN 'U' THEN COALESCE((SELECT SUM(p.rows) FROM `+d+`.sys.partitions p
			WHERE p.object_id = o.object_id AND p.index_id IN (0, 1)), 0) ELSE -1 END,
		CASE o.type WHEN 'U' THEN COALESCE((SELECT SUM(a.total_pages) FROM `+d+`.sys.partitions p
			JOIN `+d+`.sys.allocation_units a ON a.container_id = p.partition_id
			WHERE p.object_id = o.object_id), 0) * 8192 ELSE -1 END
		FROM `+d+`.sys.objects o JOIN `+d+`.sys.schemas s ON s.schema_id = o.schema_id
		WHERE o.type IN ('U', 'V') AND o.is_ms_shipped = 0
		ORDER BY CASE s.name WHEN 'dbo' THEN 0 ELSE 1 END, s.name, o.name`)
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
			THEN 1 ELSE 0 END,
		COALESCE((SELECT CAST(ep.value AS nvarchar(4000)) FROM `+d+`.sys.extended_properties ep
			JOIN `+d+`.sys.columns sc ON sc.object_id = ep.major_id AND sc.column_id = ep.minor_id
			WHERE ep.class = 1 AND ep.name = 'MS_Description' AND sc.name = c.COLUMN_NAME
			AND ep.major_id = OBJECT_ID(QUOTENAME(c.TABLE_CATALOG) + '.' + QUOTENAME(c.TABLE_SCHEMA) + '.' + QUOTENAME(c.TABLE_NAME))), ''),
		CASE WHEN EXISTS (SELECT 1 FROM `+d+`.sys.indexes i
			JOIN `+d+`.sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
			JOIN `+d+`.sys.columns sc ON sc.object_id = ic.object_id AND sc.column_id = ic.column_id
			WHERE i.is_unique = 1 AND i.is_primary_key = 0 AND ic.is_included_column = 0 AND sc.name = c.COLUMN_NAME
			AND i.object_id = OBJECT_ID(QUOTENAME(c.TABLE_CATALOG) + '.' + QUOTENAME(c.TABLE_SCHEMA) + '.' + QUOTENAME(c.TABLE_NAME)))
			THEN 1 ELSE 0 END,
		(SELECT TOP 1 rt.name + '.' + rc.name FROM `+d+`.sys.foreign_key_columns fkc
			JOIN `+d+`.sys.columns pc ON pc.object_id = fkc.parent_object_id AND pc.column_id = fkc.parent_column_id
			JOIN `+d+`.sys.objects rt ON rt.object_id = fkc.referenced_object_id
			JOIN `+d+`.sys.columns rc ON rc.object_id = fkc.referenced_object_id AND rc.column_id = fkc.referenced_column_id
			WHERE pc.name = c.COLUMN_NAME
			AND fkc.parent_object_id = OBJECT_ID(QUOTENAME(c.TABLE_CATALOG) + '.' + QUOTENAME(c.TABLE_SCHEMA) + '.' + QUOTENAME(c.TABLE_NAME)))
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
func (mssqlDialect) Select(q, where, orderBy string, n int) string {
	if n <= 0 {
		return selectWithLimit(q, where, orderBy, "")
	}
	sql := fmt.Sprintf("SELECT TOP %d * FROM %s", n, q)
	if w := strings.TrimSpace(where); w != "" {
		sql += " WHERE " + w
	}
	if o := strings.TrimSpace(orderBy); o != "" {
		sql += " ORDER BY " + o
	}
	return sql
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
	// num_rows comes from optimizer statistics (NULL until gathered).
	// Sizes come from USER_SEGMENTS, so only for the login user's own schema
	// (DBA_SEGMENTS would need extra privileges).
	return scanTables(ctx, q, `SELECT o.name, o.kind, c.comments, o.num_rows, NVL(sg.bytes, -1) FROM (
			SELECT table_name name, 'TABLE' kind, num_rows FROM all_tables WHERE owner = :1 AND table_name NOT LIKE 'BIN$%'
			UNION ALL SELECT view_name, 'VIEW', NULL FROM all_views WHERE owner = :2
			UNION ALL SELECT mview_name, 'MATERIALIZED VIEW', NULL FROM all_mviews WHERE owner = :3
		) o LEFT JOIN all_tab_comments c ON c.owner = :4 AND c.table_name = o.name
		LEFT JOIN (SELECT segment_name, SUM(bytes) bytes FROM user_segments GROUP BY segment_name) sg
			ON sg.segment_name = o.name AND o.kind = 'TABLE' AND USER = :5
		ORDER BY 1`, schema, schema, schema, schema, schema)
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
			THEN 1 ELSE 0 END,
		cc.comments,
		CASE WHEN EXISTS (SELECT 1 FROM all_constraints k JOIN all_cons_columns kc ON kc.owner = k.owner AND kc.constraint_name = k.constraint_name
			WHERE k.owner = c.owner AND k.table_name = c.table_name AND k.constraint_type = 'U' AND kc.column_name = c.column_name)
			THEN 1 ELSE 0 END,
		(SELECT MIN(rc.table_name || '.' || rc.column_name) FROM all_constraints k
			JOIN all_cons_columns kc ON kc.owner = k.owner AND kc.constraint_name = k.constraint_name
			JOIN all_cons_columns rc ON rc.owner = k.r_owner AND rc.constraint_name = k.r_constraint_name AND rc.position = kc.position
			WHERE k.owner = c.owner AND k.table_name = c.table_name AND k.constraint_type = 'R' AND kc.column_name = c.column_name)
		FROM all_tab_columns c
		LEFT JOIN all_col_comments cc ON cc.owner = c.owner AND cc.table_name = c.table_name AND cc.column_name = c.column_name
		WHERE c.owner = :1 AND c.table_name = :2 ORDER BY c.column_id`, schema, t.Name)
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
func (oracleDialect) Select(q, where, orderBy string, n int) string {
	// ROWNUM works on every version, including 11g; with ORDER BY the limit
	// has to wrap a subquery so it applies after sorting.
	if n <= 0 {
		return selectWithLimit(q, where, orderBy, "")
	}
	if strings.TrimSpace(orderBy) == "" {
		if strings.TrimSpace(where) == "" {
			return fmt.Sprintf("SELECT * FROM %s WHERE ROWNUM <= %d", q, n)
		}
		return fmt.Sprintf("SELECT * FROM %s WHERE (%s) AND ROWNUM <= %d", q, strings.TrimSpace(where), n)
	}
	return fmt.Sprintf("SELECT * FROM (%s) WHERE ROWNUM <= %d", selectWithLimit(q, where, orderBy, ""), n)
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
	return scanTables(ctx, q, `SELECT name, upper(type), '', -1, -1 FROM `+quoteWith(schema, `"`, `"`)+`.sqlite_master
		WHERE type IN ('table','view') AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
}

func (sqliteDialect) ListColumns(ctx context.Context, q Querier, schema string, t Table) ([]Column, error) {
	return scanColumns(ctx, q, `SELECT p.name, p.type, NOT p."notnull", p.pk > 0, '',
		EXISTS (SELECT 1 FROM pragma_index_list(?1, ?2) il, pragma_index_info(il.name, ?2) ii
			WHERE il."unique" = 1 AND il.origin <> 'pk' AND ii.name = p.name),
		(SELECT f."table" || '.' || f."to" FROM pragma_foreign_key_list(?1, ?2) f WHERE f."from" = p.name)
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
func (sqliteDialect) Select(q, where, orderBy string, n int) string {
	return selectWithLimit(q, where, orderBy, limitClause(n))
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

func limitClause(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf(" LIMIT %d", n)
}

func selectWithLimit(q, where, orderBy, limit string) string {
	sql := "SELECT * FROM " + q
	if w := strings.TrimSpace(where); w != "" {
		sql += " WHERE " + w
	}
	if o := strings.TrimSpace(orderBy); o != "" {
		sql += " ORDER BY " + o
	}
	return sql + limit
}
