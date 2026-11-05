//go:build !no_postgres

package dbx

// PostgreSQL: connection string, metadata, DDL, column changes, routines
// and bind parameters. Left out of builds with the no_postgres tag.

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func init() { register(pgDialect{}, "postgres") }

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
		COALESCE(obj_description(c.oid, 'pg_class'), ''), -1, -1
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN ('r','v','m','p','f') ORDER BY c.relname`, schema)
}

func (pgDialect) TableStats(ctx context.Context, q Querier, schema string) ([]Table, error) {
	return scanStats(ctx, q, `SELECT c.relname,
		CASE WHEN c.reltuples < 0 THEN -1 ELSE c.reltuples::bigint END,
		pg_total_relation_size(c.oid)
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN ('r','m','p')`, schema)
}

func (pgDialect) ListColumns(ctx context.Context, q Querier, schema string, t Table) ([]Column, error) {
	return scanColumns(ctx, q, `SELECT a.attname, format_type(a.atttypid, a.atttypmod), NOT a.attnotnull,
		EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid = a.attrelid AND i.indisprimary AND a.attnum = ANY(i.indkey)),
		COALESCE(col_description(a.attrelid, a.attnum), ''),
		EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid = a.attrelid AND i.indisunique AND NOT i.indisprimary AND a.attnum = ANY(i.indkey)),
		(SELECT CASE WHEN rc.relnamespace <> c.relnamespace THEN (SELECT rn.nspname FROM pg_namespace rn WHERE rn.oid = rc.relnamespace) || '.' ELSE '' END || rc.relname || '.' || ra.attname FROM pg_constraint con
			JOIN pg_class rc ON rc.oid = con.confrelid
			JOIN pg_attribute ra ON ra.attrelid = con.confrelid AND ra.attnum = con.confkey[array_position(con.conkey, a.attnum)]
			WHERE con.conrelid = a.attrelid AND con.contype = 'f' AND a.attnum = ANY(con.conkey) LIMIT 1),
		(SELECT pg_get_expr(d.adbin, d.adrelid) FROM pg_attrdef d WHERE d.adrelid = a.attrelid AND d.adnum = a.attnum),
		CASE a.attidentity WHEN 'a' THEN 'identity always' WHEN 'd' THEN 'identity by default' ELSE
			CASE a.attgenerated WHEN 's' THEN 'generated' ELSE '' END END
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
func (pgDialect) Select(q, where, orderBy string, n, offset int) string {
	return selectWithLimit(q, where, orderBy, limitClause(n, offset))
}
func (pgDialect) Version(ctx context.Context, q Querier) string {
	return firstString(ctx, q, "SHOW server_version")
}

func (pgDialect) DDL(ctx context.Context, q *sql.Conn, schema string, t Table) (string, error) {
	var oid int64
	var kind string
	err := q.QueryRowContext(ctx, `SELECT c.oid, c.relkind::text FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2`, schema, t.Name).Scan(&oid, &kind)
	if err != nil {
		return "", err
	}
	name := pgDialect{}.QualifiedName(schema, t)
	var b strings.Builder
	switch kind {
	case "v", "m":
		var def string
		if err := q.QueryRowContext(ctx, "SELECT pg_get_viewdef($1::oid, true)", oid).Scan(&def); err != nil {
			return "", err
		}
		if kind == "v" {
			fmt.Fprintf(&b, "CREATE OR REPLACE VIEW %s AS\n%s", name, strings.TrimSpace(def))
		} else {
			fmt.Fprintf(&b, "CREATE MATERIALIZED VIEW %s AS\n%s", name, strings.TrimSpace(def))
		}
		if !strings.HasSuffix(b.String(), ";") {
			b.WriteString(";")
		}
	default:
		cols, err := rowStrings(ctx, q, `SELECT quote_ident(a.attname), format_type(a.atttypid, a.atttypmod),
			a.attnotnull::text, COALESCE(pg_get_expr(d.adbin, d.adrelid), ''), a.attidentity::text, a.attgenerated::text
			FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
			WHERE a.attrelid = $1::oid AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum`, oid)
		if err != nil {
			return "", err
		}
		var lines []string
		for _, c := range cols {
			l := "    " + c[0] + " " + c[1]
			switch {
			case c[4] == "a":
				l += " GENERATED ALWAYS AS IDENTITY"
			case c[4] == "d":
				l += " GENERATED BY DEFAULT AS IDENTITY"
			case c[5] == "s":
				l += " GENERATED ALWAYS AS (" + c[3] + ") STORED"
			case c[3] != "":
				l += " DEFAULT " + c[3]
			}
			if c[2] == "true" {
				l += " NOT NULL"
			}
			lines = append(lines, l)
		}
		cons, err := rowStrings(ctx, q, `SELECT quote_ident(conname), pg_get_constraintdef(oid, true) FROM pg_constraint
			WHERE conrelid = $1::oid ORDER BY CASE contype WHEN 'p' THEN 0 WHEN 'u' THEN 1 WHEN 'c' THEN 2 ELSE 3 END, conname`, oid)
		if err != nil {
			return "", err
		}
		for _, c := range cons {
			lines = append(lines, "    CONSTRAINT "+c[0]+" "+c[1])
		}
		fmt.Fprintf(&b, "CREATE TABLE %s (\n%s\n);", name, strings.Join(lines, ",\n"))
		idx, err := rowStrings(ctx, q, `SELECT pg_get_indexdef(indexrelid) FROM pg_index
			WHERE indrelid = $1::oid AND indexrelid NOT IN (SELECT conindid FROM pg_constraint WHERE conrelid = $1::oid)
			ORDER BY indexrelid`, oid)
		if err != nil {
			return "", err
		}
		if len(idx) > 0 {
			b.WriteString("\n")
		}
		for _, i := range idx {
			b.WriteString("\n" + i[0] + ";")
		}
	}
	comments, err := rowStrings(ctx, q, `SELECT '', obj_description($1::oid, 'pg_class') WHERE obj_description($1::oid, 'pg_class') IS NOT NULL
		UNION ALL SELECT quote_ident(a.attname), col_description($1::oid, a.attnum) FROM pg_attribute a
		WHERE a.attrelid = $1::oid AND a.attnum > 0 AND NOT a.attisdropped AND col_description($1::oid, a.attnum) IS NOT NULL`, oid)
	if err == nil && len(comments) > 0 {
		b.WriteString("\n")
		for _, c := range comments {
			lit := "'" + strings.ReplaceAll(c[1], "'", "''") + "'"
			if c[0] == "" {
				fmt.Fprintf(&b, "\nCOMMENT ON TABLE %s IS %s;", name, lit)
			} else {
				fmt.Fprintf(&b, "\nCOMMENT ON COLUMN %s.%s IS %s;", name, c[0], lit)
			}
		}
	}
	return b.String(), nil
}

func (pgDialect) transactionalDDL() bool { return true }

func (pgDialect) alterColumn(ctx context.Context, q Querier, schema string, t Table, target string, a ColumnAlter) ([]string, error) {
	d := diffOf(a)
	col := quoteWith(a.Old.Name, `"`, `"`)
	head := "ALTER TABLE " + target + " ALTER COLUMN " + col
	var out []string
	if d.typ {
		// USING converts existing values where there is no implicit cast
		out = append(out, head+" TYPE "+a.New.Type+" USING "+col+"::"+a.New.Type)
	}
	if d.null {
		if a.New.Nullable {
			out = append(out, head+" DROP NOT NULL")
		} else {
			out = append(out, head+" SET NOT NULL")
		}
	}
	// an identity takes no default: it goes before a new default is set,
	// and comes after the old one is dropped
	if d.auto && !autoInc(a.New) {
		out = append(out, head+" DROP IDENTITY")
	}
	if d.def {
		if v := defaultOf(a.New.Default); v != "" {
			out = append(out, head+" SET DEFAULT "+v)
		} else {
			out = append(out, head+" DROP DEFAULT")
		}
	}
	if d.auto && autoInc(a.New) { // also needs NOT NULL, set above
		out = append(out, head+" ADD GENERATED BY DEFAULT AS IDENTITY")
	}
	if d.comment {
		c := "NULL"
		if a.New.Comment != "" {
			c = sqlQuote(a.New.Comment, false)
		}
		out = append(out, "COMMENT ON COLUMN "+target+"."+col+" IS "+c)
	}
	if d.name { // last: the statements above name the column as it was
		n := a.New.Name
		if plainIdent.MatchString(n) {
			n = strings.ToLower(n)
		}
		if n != a.Old.Name { // "id" typed as ID folds back to the name it has
			out = append(out, "ALTER TABLE "+target+" RENAME COLUMN "+col+" TO "+quoteWith(n, `"`, `"`))
		}
	}
	return out, nil
}

func (d pgDialect) addColumn(schema string, t Table, target string, c Column) []string {
	def, after := d.columnDef(schema, t, target, c)
	return append([]string{"ALTER TABLE " + target + " ADD COLUMN " + def}, after...)
}

// columnDef: a new column's definition, then its comment.
func (pgDialect) columnDef(_ string, _ Table, target string, c Column) (string, []string) {
	c.Name = foldName("postgres", c.Name)
	col := quoteWith(c.Name, `"`, `"`)
	def := col + " " + c.Type
	if autoInc(c) {
		def += " GENERATED BY DEFAULT AS IDENTITY"
	}
	if !c.Nullable {
		def += " NOT NULL"
	}
	if c.Default != "" {
		def += " DEFAULT " + c.Default
	}
	var after []string
	if c.Comment != "" {
		after = append(after, "COMMENT ON COLUMN "+target+"."+col+" IS "+sqlQuote(c.Comment, false))
	}
	return def, after
}

func (pgDialect) dropColumn(_ context.Context, _ Querier, _ string, _ Table, target, name string) ([]string, error) {
	return []string{"ALTER TABLE " + target + " DROP COLUMN " + quoteWith(name, `"`, `"`)}, nil
}

func (pgDialect) tableComment(_ string, _ Table, target, comment string) []string {
	c := "NULL"
	if comment != "" {
		c = sqlQuote(comment, false)
	}
	return []string{"COMMENT ON TABLE " + target + " IS " + c}
}

func (pgDialect) Routines(ctx context.Context, q Querier, schema string) ([]Routine, error) {
	return scanRoutines(ctx, q, `SELECT n.nspname, p.proname,
		CASE p.prokind WHEN 'p' THEN 'PROCEDURE' ELSE 'FUNCTION' END,
		pg_get_function_identity_arguments(p.oid), p.oid::text
		FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = $1 AND p.prokind IN ('f', 'p')
		ORDER BY 3 DESC, 2, 4`, schema)
}

func (pgDialect) RoutineSource(ctx context.Context, conn *sql.Conn, schema string, r Routine) (string, error) {
	var src sql.NullString
	err := conn.QueryRowContext(ctx, "SELECT pg_get_functiondef($1::oid)", r.ID).Scan(&src)
	if err != nil {
		return "", err
	}
	// The body is dollar-quoted, so it already runs as one statement.
	return strings.TrimSpace(src.String) + ";", nil
}

func (pgDialect) Param(n int, typ string) string {
	if typ == "" {
		return fmt.Sprintf("$%d", n)
	}
	return fmt.Sprintf("CAST($%d::text AS %s)", n, typ)
}

// ---- indexes

func (pgDialect) listIndexes(ctx context.Context, q Querier, schema string, t Table) ([]Index, error) {
	rows, err := rowStrings(ctx, q, `SELECT ic.relname, i.indisunique, i.indisprimary, COALESCE(con.conname, ''), am.amname,
		COALESCE(pg_get_expr(i.indpred, i.indrelid, true), ''),
		k <= i.indnkeyatts,
		pg_get_indexdef(i.indexrelid, k, true) || CASE WHEN k <= i.indnkeyatts AND i.indoption[k - 1] & 1 = 1 THEN ' DESC' ELSE '' END
		FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_class c ON c.oid = i.indrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_am am ON am.oid = ic.relam
		LEFT JOIN pg_constraint con ON con.conindid = i.indexrelid AND con.conrelid = i.indrelid AND con.contype IN ('p', 'u', 'x')
		CROSS JOIN LATERAL generate_series(1, i.indnatts) k
		WHERE n.nspname = $1 AND c.relname = $2
		ORDER BY i.indisprimary DESC, ic.relname, k`, schema, t.Name)
	if err != nil {
		return nil, err
	}
	yes := func(s string) bool { return s == "true" || s == "t" }
	return groupIndexes(rows, func(r []string) Index {
		return Index{Name: r[0], Unique: yes(r[1]), Primary: yes(r[2]), Constraint: r[3], Type: strings.ToUpper(r[4]), Where: r[5]}
	}, func(ix *Index, r []string) {
		if yes(r[6]) {
			ix.Columns = append(ix.Columns, r[7])
		} else {
			ix.Include = append(ix.Include, r[7])
		}
	}), nil
}

func (d pgDialect) createIndex(schema string, t Table, ix NewIndex) string {
	quote := func(s string) string { return quoteWith(s, `"`, `"`) }
	return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)", uniqueKw(ix.Unique), quote(ix.Name), d.QualifiedName(schema, t), indexColumns(ix, quote))
}

func (d pgDialect) dropIndex(schema string, t Table, ix Index) (string, error) {
	if ix.Constraint != "" {
		return fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", d.QualifiedName(schema, t), quoteWith(ix.Constraint, `"`, `"`)), nil
	}
	return fmt.Sprintf("DROP INDEX %s.%s", quoteWith(schema, `"`, `"`), quoteWith(ix.Name, `"`, `"`)), nil
}

func (d pgDialect) renameTable(schema string, t Table, name string) string {
	kind := "TABLE"
	switch strings.ToUpper(t.Type) {
	case "VIEW":
		kind = "VIEW"
	case "MATERIALIZED VIEW":
		kind = "MATERIALIZED VIEW"
	}
	return fmt.Sprintf("ALTER %s %s RENAME TO %s", kind, d.QualifiedName(schema, t), quoteWith(name, `"`, `"`))
}
