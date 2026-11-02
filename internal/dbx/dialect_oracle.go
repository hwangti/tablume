//go:build !no_oracle

package dbx

// Oracle: connection string, metadata, DDL, column changes, routines
// and bind parameters. Left out of builds with the no_oracle tag.

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	goora "github.com/sijms/go-ora/v2"
)

func init() { register(oracleDialect{}, "oracle") }

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
	// Separate simple queries merged here: on a large data dictionary a
	// join of the ALL_* views can get a bad plan and take many seconds.
	// A materialized view also has a container table of the same name in
	// ALL_TABLES: list it once, as the materialized view.
	tables, err := scanStrings(ctx, q, `SELECT table_name FROM all_tables WHERE owner = :1 AND table_name NOT LIKE 'BIN$%'`, schema)
	if err != nil {
		return nil, err
	}
	views, err := scanStrings(ctx, q, `SELECT view_name FROM all_views WHERE owner = :1`, schema)
	if err != nil {
		return nil, err
	}
	mviews, err := scanStrings(ctx, q, `SELECT mview_name FROM all_mviews WHERE owner = :1`, schema)
	if err != nil {
		return nil, err
	}
	comments := map[string]string{}
	rows, err := q.QueryContext(ctx, `SELECT table_name, comments FROM all_tab_comments WHERE owner = :1 AND comments IS NOT NULL`, schema)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n, c sql.NullString
		if err := rows.Scan(&n, &c); err != nil {
			rows.Close()
			return nil, err
		}
		comments[n.String] = c.String
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	isMview := make(map[string]bool, len(mviews))
	for _, m := range mviews {
		isMview[m] = true
	}
	out := make([]Table, 0, len(tables)+len(views)+len(mviews))
	add := func(name, typ string) {
		out = append(out, Table{Name: name, Type: typ, Comment: comments[name], Rows: -1, Size: -1})
	}
	for _, t := range tables {
		if isMview[t] {
			add(t, "MATERIALIZED VIEW")
			delete(isMview, t)
		} else {
			add(t, "TABLE")
		}
	}
	for _, v := range views {
		add(v, "VIEW")
	}
	for _, m := range mviews {
		if isMview[m] { // no container table visible
			add(m, "MATERIALIZED VIEW")
		}
	}
	return out, nil
}

func (oracleDialect) TableStats(ctx context.Context, q Querier, schema string) ([]Table, error) {
	// Rows and size both come from optimizer statistics (NULL until
	// gathered): size is the table's blocks times the tablespace block
	// size. USER_SEGMENTS / DBA_SEGMENTS would be exact but can take many
	// seconds on a schema with lots of segments.
	return scanStats(ctx, q, `SELECT t.table_name, t.num_rows,
		CASE WHEN t.blocks IS NULL THEN -1 ELSE t.blocks * NVL(ts.block_size, 8192) END
		FROM all_tables t LEFT JOIN user_tablespaces ts ON ts.tablespace_name = t.tablespace_name
		WHERE t.owner = :1`, schema)
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
			WHERE k.owner = c.owner AND k.table_name = c.table_name AND k.constraint_type = 'R' AND kc.column_name = c.column_name),
		c.data_default
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
func (oracleDialect) Select(q, where, orderBy string, n, offset int) string {
	// Later pages use OFFSET … FETCH (12c+).
	if offset > 0 {
		fetch := ""
		if n > 0 {
			fetch = fmt.Sprintf(" FETCH NEXT %d ROWS ONLY", n)
		}
		return selectWithLimit(q, where, orderBy, fmt.Sprintf(" OFFSET %d ROWS", offset)+fetch)
	}
	// The first page uses ROWNUM, which works on every version including
	// 11g; with ORDER BY the limit has to wrap a subquery so it applies
	// after sorting.
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

func (oracleDialect) DDL(ctx context.Context, q *sql.Conn, schema string, t Table) (string, error) {
	// Session-level transforms: leave out storage clutter, end with ';'.
	_, _ = q.ExecContext(ctx, `BEGIN
		DBMS_METADATA.SET_TRANSFORM_PARAM(DBMS_METADATA.SESSION_TRANSFORM, 'SEGMENT_ATTRIBUTES', FALSE);
		DBMS_METADATA.SET_TRANSFORM_PARAM(DBMS_METADATA.SESSION_TRANSFORM, 'STORAGE', FALSE);
		DBMS_METADATA.SET_TRANSFORM_PARAM(DBMS_METADATA.SESSION_TRANSFORM, 'SQLTERMINATOR', TRUE);
		DBMS_METADATA.SET_TRANSFORM_PARAM(DBMS_METADATA.SESSION_TRANSFORM, 'PRETTY', TRUE);
	END;`)
	objType := "TABLE"
	switch t.Type {
	case "VIEW":
		objType = "VIEW"
	case "MATERIALIZED VIEW":
		objType = "MATERIALIZED_VIEW"
	}
	var ddl sql.NullString
	if err := q.QueryRowContext(ctx, "SELECT DBMS_METADATA.GET_DDL(:1, :2, :3) FROM dual", objType, t.Name, schema).Scan(&ddl); err != nil {
		return "", err
	}
	parts := []string{strings.TrimSpace(ddl.String)}
	if objType == "TABLE" {
		// Indexes that don't back a constraint (those are already in the table DDL).
		idx, _ := scanStrings(ctx, q, `SELECT index_name FROM all_indexes i WHERE owner = :1 AND table_name = :2
			AND index_type NOT LIKE 'LOB%'
			AND NOT EXISTS (SELECT 1 FROM all_constraints c WHERE c.owner = i.table_owner AND c.table_name = i.table_name AND c.index_name = i.index_name)
			ORDER BY index_name`, schema, t.Name)
		for _, name := range idx {
			var d sql.NullString
			if q.QueryRowContext(ctx, "SELECT DBMS_METADATA.GET_DDL('INDEX', :1, :2) FROM dual", name, schema).Scan(&d) == nil {
				parts = append(parts, strings.TrimSpace(d.String))
			}
		}
		var cm sql.NullString
		if q.QueryRowContext(ctx, "SELECT DBMS_METADATA.GET_DEPENDENT_DDL('COMMENT', :1, :2) FROM dual", t.Name, schema).Scan(&cm) == nil {
			parts = append(parts, strings.TrimSpace(cm.String))
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

func (oracleDialect) transactionalDDL() bool { return false }

func (oracleDialect) alterColumn(ctx context.Context, q Querier, schema string, t Table, target string, a ColumnAlter) ([]string, error) {
	d := diffOf(a)
	col := quoteWith(a.Old.Name, `"`, `"`)
	var parts []string
	if d.typ {
		parts = append(parts, a.New.Type)
	}
	if d.def {
		v := defaultOf(a.New.Default)
		if v == "" {
			v = "NULL"
		}
		parts = append(parts, "DEFAULT "+v)
	}
	// Only when it changes: restating the current NULL-ness is an error (ORA-01442/01451).
	if d.null {
		if a.New.Nullable {
			parts = append(parts, "NULL")
		} else {
			parts = append(parts, "NOT NULL")
		}
	}
	var out []string
	if len(parts) > 0 {
		out = append(out, "ALTER TABLE "+target+" MODIFY ("+col+" "+strings.Join(parts, " ")+")")
	}
	if d.comment {
		out = append(out, "COMMENT ON COLUMN "+target+"."+col+" IS "+sqlQuote(a.New.Comment, false))
	}
	return out, nil
}

func (oracleDialect) Routines(ctx context.Context, q Querier, schema string) ([]Routine, error) {
	return scanRoutines(ctx, q, `SELECT owner, object_name, object_type, '', ''
		FROM all_objects WHERE owner = :1 AND object_type IN ('PROCEDURE', 'FUNCTION', 'PACKAGE')
		ORDER BY CASE object_type WHEN 'PACKAGE' THEN 0 WHEN 'PROCEDURE' THEN 1 ELSE 2 END, object_name`, schema)
}

func (oracleDialect) RoutineSource(ctx context.Context, conn *sql.Conn, schema string, r Routine) (string, error) {
	_, _ = conn.ExecContext(ctx, `BEGIN
		DBMS_METADATA.SET_TRANSFORM_PARAM(DBMS_METADATA.SESSION_TRANSFORM, 'SQLTERMINATOR', TRUE);
		DBMS_METADATA.SET_TRANSFORM_PARAM(DBMS_METADATA.SESSION_TRANSFORM, 'PRETTY', TRUE);
	END;`)
	var src sql.NullString
	// PACKAGE returns the specification and the body together.
	if err := conn.QueryRowContext(ctx, "SELECT DBMS_METADATA.GET_DDL(:1, :2, :3) FROM dual", r.Type, r.Name, schema).Scan(&src); err != nil {
		return "", err
	}
	return strings.TrimSpace(src.String), nil
}

func (oracleDialect) Param(n int, typ string) string {
	u := strings.ToUpper(typ)
	switch {
	case u == "DATE":
		return fmt.Sprintf("TO_DATE(:%d, 'YYYY-MM-DD HH24:MI:SS')", n)
	case strings.HasPrefix(u, "TIMESTAMP") && strings.Contains(u, "TIME ZONE"):
		return fmt.Sprintf("TO_TIMESTAMP_TZ(:%d, 'YYYY-MM-DD HH24:MI:SS.FF TZH:TZM')", n)
	case strings.HasPrefix(u, "TIMESTAMP"):
		return fmt.Sprintf("TO_TIMESTAMP(:%d, 'YYYY-MM-DD HH24:MI:SS.FF')", n)
	}
	return fmt.Sprintf(":%d", n)
}
