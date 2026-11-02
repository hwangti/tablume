package dbx

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Dialect hides the differences between database engines.
type Dialect interface {
	DriverName() string
	DSN(c ConnConfig) string
	Style() string // statement splitting style, see Split
	ListSchemas(ctx context.Context, q Querier) (SchemaList, error)
	CurrentSchema(ctx context.Context, q Querier) string
	// ListTables lists names, types and comments; Rows and Size are -1
	// (see TableStats, which is slower on large catalogs).
	ListTables(ctx context.Context, q Querier, schema string) ([]Table, error)
	ListColumns(ctx context.Context, q Querier, schema string, t Table) ([]Column, error)
	// AllColumns returns table name -> column names, used for autocompletion.
	AllColumns(ctx context.Context, q Querier, schema string) (map[string][]string, error)
	UseSchema(schema string) string
	QualifiedName(schema string, t Table) string
	// Select builds SELECT * with an optional WHERE / ORDER BY and a row
	// limit; n <= 0 means no limit (rows are then fetched incrementally).
	// Select builds a table data query: n rows (n <= 0: all) after skipping offset.
	Select(qualified, where, orderBy string, n, offset int) string
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

// Each engine lives in its own dialect_<name>.go and registers itself.
// A build can leave engines out with tags: no_mysql (MySQL and MariaDB),
// no_postgres, no_mssql, no_oracle, no_sqlite.
var dialects = map[string]Dialect{}

func register(d Dialect, drivers ...string) {
	for _, n := range drivers {
		dialects[n] = d
	}
}

var driverLabels = map[string]string{
	"mysql": "MySQL", "mariadb": "MariaDB", "postgres": "PostgreSQL",
	"mssql": "SQL Server", "oracle": "Oracle", "sqlite": "SQLite",
}

func DialectFor(driver string) (Dialect, error) {
	if d, ok := dialects[driver]; ok {
		return d, nil
	}
	if label, ok := driverLabels[driver]; ok {
		return nil, fmt.Errorf("이 빌드에는 %s 지원이 들어 있지 않습니다", label)
	}
	return nil, fmt.Errorf("지원하지 않는 드라이버: %s", driver)
}

// SupportedDrivers lists the drivers in this build.
func SupportedDrivers() []string {
	out := make([]string, 0, len(dialects))
	for k := range dialects {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// msq quotes a SQL Server identifier (also used for the generic quoting).
func msq(s string) string { return quoteWith(s, "[", "]") }

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

// scanStats reads (name, rows, size) rows, optionally preceded by the
// inner schema (mssql), into Tables carrying just those fields.
func scanStats(ctx context.Context, q Querier, query string, args ...any) ([]Table, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	out := []Table{}
	for rows.Next() {
		var schema, name sql.NullString
		var n, size any
		if len(cols) == 4 {
			err = rows.Scan(&schema, &name, &n, &size)
		} else {
			err = rows.Scan(&name, &n, &size)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, Table{Schema: schema.String, Name: name.String, Rows: toInt64(n), Size: toInt64(size)})
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
	// name, type, nullable, pk, comment, unique, fk [, default [, extra]]
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []Column{}
	for rows.Next() {
		var name, typ, comment, fk, def, extra sql.NullString
		var nullable, pk, unique any
		dest := []any{&name, &typ, &nullable, &pk, &comment, &unique, &fk, &def, &extra}
		if err := rows.Scan(dest[:len(cols)]...); err != nil {
			return nil, err
		}
		out = append(out, Column{Name: name.String, Type: typ.String, Nullable: truthy(nullable), PK: truthy(pk),
			Comment: comment.String, Unique: truthy(unique), FK: fk.String,
			Default: strings.TrimSpace(def.String), Extra: strings.TrimSpace(extra.String)})
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

func limitClause(n, offset int) string {
	switch {
	case n > 0 && offset > 0:
		return fmt.Sprintf(" LIMIT %d OFFSET %d", n, offset)
	case n > 0:
		return fmt.Sprintf(" LIMIT %d", n)
	case offset > 0:
		// MySQL/SQLite have no OFFSET without LIMIT; PostgreSQL accepts this too.
		return fmt.Sprintf(" LIMIT %d OFFSET %d", int64(1)<<62, offset)
	}
	return ""
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
