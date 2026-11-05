//go:build !no_mssql

package dbx

// SQL Server: connection string, metadata, DDL, column changes, routines
// and bind parameters. Left out of builds with the no_mssql tag.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	_ "github.com/microsoft/go-mssqldb"
)

func init() { register(mssqlDialect{}, "mssql") }

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
			WHERE ep.class = 1 AND ep.major_id = o.object_id AND ep.minor_id = 0 AND ep.name = 'MS_Description'), ''), -1, -1
		FROM `+d+`.sys.objects o JOIN `+d+`.sys.schemas s ON s.schema_id = o.schema_id
		WHERE o.type IN ('U', 'V') AND o.is_ms_shipped = 0
		ORDER BY CASE s.name WHEN 'dbo' THEN 0 ELSE 1 END, s.name, o.name`)
}

func (mssqlDialect) TableStats(ctx context.Context, q Querier, db string) ([]Table, error) {
	d := msq(db)
	return scanStats(ctx, q, `SELECT s.name, o.name,
		COALESCE((SELECT SUM(p.rows) FROM `+d+`.sys.partitions p
			WHERE p.object_id = o.object_id AND p.index_id IN (0, 1)), 0),
		COALESCE((SELECT SUM(a.total_pages) FROM `+d+`.sys.partitions p
			JOIN `+d+`.sys.allocation_units a ON a.container_id = p.partition_id
			WHERE p.object_id = o.object_id), 0) * 8192
		FROM `+d+`.sys.objects o JOIN `+d+`.sys.schemas s ON s.schema_id = o.schema_id
		WHERE o.type = 'U' AND o.is_ms_shipped = 0`)
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
		(SELECT TOP 1 CASE WHEN SCHEMA_NAME(rt.schema_id) <> c.TABLE_SCHEMA THEN SCHEMA_NAME(rt.schema_id) + '.' ELSE '' END + rt.name + '.' + rc.name FROM `+d+`.sys.foreign_key_columns fkc
			JOIN `+d+`.sys.columns pc ON pc.object_id = fkc.parent_object_id AND pc.column_id = fkc.parent_column_id
			JOIN `+d+`.sys.objects rt ON rt.object_id = fkc.referenced_object_id
			JOIN `+d+`.sys.columns rc ON rc.object_id = fkc.referenced_object_id AND rc.column_id = fkc.referenced_column_id
			WHERE pc.name = c.COLUMN_NAME
			AND fkc.parent_object_id = OBJECT_ID(QUOTENAME(c.TABLE_CATALOG) + '.' + QUOTENAME(c.TABLE_SCHEMA) + '.' + QUOTENAME(c.TABLE_NAME))),
		c.COLUMN_DEFAULT,
		CASE WHEN COLUMNPROPERTY(OBJECT_ID(QUOTENAME(c.TABLE_CATALOG) + '.' + QUOTENAME(c.TABLE_SCHEMA) + '.' + QUOTENAME(c.TABLE_NAME)), c.COLUMN_NAME, 'IsIdentity') = 1 THEN 'identity'
			WHEN COLUMNPROPERTY(OBJECT_ID(QUOTENAME(c.TABLE_CATALOG) + '.' + QUOTENAME(c.TABLE_SCHEMA) + '.' + QUOTENAME(c.TABLE_NAME)), c.COLUMN_NAME, 'IsComputed') = 1 THEN 'computed' ELSE '' END
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
func (mssqlDialect) Select(q, where, orderBy string, n, offset int) string {
	if offset > 0 {
		// OFFSET … FETCH (SQL Server 2012+) needs an ORDER BY.
		o := strings.TrimSpace(orderBy)
		if o == "" {
			o = "(SELECT NULL)"
		}
		fetch := ""
		if n > 0 {
			fetch = fmt.Sprintf(" FETCH NEXT %d ROWS ONLY", n)
		}
		return selectWithLimit(q, where, o, fmt.Sprintf(" OFFSET %d ROWS", offset)+fetch)
	}
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

// There is no built-in "script table", so it is assembled from sys.* views.

func (mssqlDialect) DDL(ctx context.Context, q *sql.Conn, db string, t Table) (string, error) {
	d := msq(db)
	s := t.Schema
	if s == "" {
		s = "dbo"
	}
	full := msq(db) + "." + msq(s) + "." + msq(t.Name)
	objFilter := `o.name = @p2 AND sc.name = @p1`
	if strings.Contains(t.Type, "VIEW") {
		rows, err := rowStrings(ctx, q, `SELECT m.definition FROM `+d+`.sys.sql_modules m
			JOIN `+d+`.sys.objects o ON o.object_id = m.object_id JOIN `+d+`.sys.schemas sc ON sc.schema_id = o.schema_id
			WHERE `+objFilter, s, t.Name)
		if err != nil || len(rows) == 0 {
			return "", err
		}
		return strings.TrimSpace(rows[0][0]) + "\nGO", nil
	}

	cols, err := rowStrings(ctx, q, `SELECT c.name, ty.name, CAST(c.max_length AS int), CAST(c.precision AS int), CAST(c.scale AS int),
		CAST(c.is_nullable AS int), CAST(c.is_identity AS int), CAST(ic.seed_value AS nvarchar(40)), CAST(ic.increment_value AS nvarchar(40)),
		dc.name, dc.definition, cc.definition
		FROM `+d+`.sys.columns c
		JOIN `+d+`.sys.objects o ON o.object_id = c.object_id
		JOIN `+d+`.sys.schemas sc ON sc.schema_id = o.schema_id
		JOIN `+d+`.sys.types ty ON ty.user_type_id = c.user_type_id
		LEFT JOIN `+d+`.sys.identity_columns ic ON ic.object_id = c.object_id AND ic.column_id = c.column_id
		LEFT JOIN `+d+`.sys.default_constraints dc ON dc.object_id = c.default_object_id
		LEFT JOIN `+d+`.sys.computed_columns cc ON cc.object_id = c.object_id AND cc.column_id = c.column_id
		WHERE `+objFilter+` ORDER BY c.column_id`, s, t.Name)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, c := range cols {
		if c[11] != "" {
			lines = append(lines, "    "+msq(c[0])+" AS "+c[11])
			continue
		}
		l := "    " + msq(c[0]) + " " + mssqlType(c[1], c[2], c[3], c[4])
		if c[6] == "1" {
			l += fmt.Sprintf(" IDENTITY(%s,%s)", c[7], c[8])
		}
		if c[5] == "1" {
			l += " NULL"
		} else {
			l += " NOT NULL"
		}
		if c[10] != "" {
			l += " CONSTRAINT " + msq(c[9]) + " DEFAULT " + c[10]
		}
		lines = append(lines, l)
	}

	// PRIMARY KEY / UNIQUE constraints
	keys, err := rowStrings(ctx, q, `SELECT kc.name, kc.type, i.type_desc, col.name, CAST(ic.is_descending_key AS int)
		FROM `+d+`.sys.key_constraints kc
		JOIN `+d+`.sys.objects o ON o.object_id = kc.parent_object_id JOIN `+d+`.sys.schemas sc ON sc.schema_id = o.schema_id
		JOIN `+d+`.sys.indexes i ON i.object_id = kc.parent_object_id AND i.index_id = kc.unique_index_id
		JOIN `+d+`.sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
		JOIN `+d+`.sys.columns col ON col.object_id = ic.object_id AND col.column_id = ic.column_id
		WHERE `+objFilter+` ORDER BY kc.type, kc.name, ic.key_ordinal`, s, t.Name)
	if err != nil {
		return "", err
	}
	for _, g := range groupRows(keys, 0) {
		kind := "UNIQUE"
		if strings.TrimSpace(g[0][1]) == "PK" {
			kind = "PRIMARY KEY"
		}
		lines = append(lines, fmt.Sprintf("    CONSTRAINT %s %s %s (%s)", msq(g[0][0]), kind, g[0][2], joinCols(g, 3, 4)))
	}

	// FOREIGN KEYs
	fks, err := rowStrings(ctx, q, `SELECT fk.name, pc.name, rs.name, rt.name, rc.name, fk.delete_referential_action_desc, fk.update_referential_action_desc
		FROM `+d+`.sys.foreign_keys fk
		JOIN `+d+`.sys.objects o ON o.object_id = fk.parent_object_id JOIN `+d+`.sys.schemas sc ON sc.schema_id = o.schema_id
		JOIN `+d+`.sys.foreign_key_columns fkc ON fkc.constraint_object_id = fk.object_id
		JOIN `+d+`.sys.columns pc ON pc.object_id = fkc.parent_object_id AND pc.column_id = fkc.parent_column_id
		JOIN `+d+`.sys.objects rt ON rt.object_id = fk.referenced_object_id JOIN `+d+`.sys.schemas rs ON rs.schema_id = rt.schema_id
		JOIN `+d+`.sys.columns rc ON rc.object_id = fkc.referenced_object_id AND rc.column_id = fkc.referenced_column_id
		WHERE `+objFilter+` ORDER BY fk.name, fkc.constraint_column_id`, s, t.Name)
	if err != nil {
		return "", err
	}
	for _, g := range groupRows(fks, 0) {
		var pcs, rcs []string
		for _, r := range g {
			pcs = append(pcs, msq(r[1]))
			rcs = append(rcs, msq(r[4]))
		}
		l := fmt.Sprintf("    CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s.%s (%s)", msq(g[0][0]), strings.Join(pcs, ", "),
			msq(g[0][2]), msq(g[0][3]), strings.Join(rcs, ", "))
		if a := g[0][5]; a != "NO_ACTION" {
			l += " ON DELETE " + strings.ReplaceAll(a, "_", " ")
		}
		if a := g[0][6]; a != "NO_ACTION" {
			l += " ON UPDATE " + strings.ReplaceAll(a, "_", " ")
		}
		lines = append(lines, l)
	}

	checks, err := rowStrings(ctx, q, `SELECT ck.name, ck.definition FROM `+d+`.sys.check_constraints ck
		JOIN `+d+`.sys.objects o ON o.object_id = ck.parent_object_id JOIN `+d+`.sys.schemas sc ON sc.schema_id = o.schema_id
		WHERE `+objFilter+` ORDER BY ck.name`, s, t.Name)
	if err != nil {
		return "", err
	}
	for _, c := range checks {
		lines = append(lines, fmt.Sprintf("    CONSTRAINT %s CHECK %s", msq(c[0]), c[1]))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE %s (\n%s\n);", full, strings.Join(lines, ",\n"))

	idx, err := rowStrings(ctx, q, `SELECT i.name, CAST(i.is_unique AS int), i.type_desc, col.name, CAST(ic.is_descending_key AS int),
		CAST(ic.is_included_column AS int), COALESCE(i.filter_definition, '')
		FROM `+d+`.sys.indexes i
		JOIN `+d+`.sys.objects o ON o.object_id = i.object_id JOIN `+d+`.sys.schemas sc ON sc.schema_id = o.schema_id
		JOIN `+d+`.sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
		JOIN `+d+`.sys.columns col ON col.object_id = ic.object_id AND col.column_id = ic.column_id
		WHERE `+objFilter+` AND i.is_primary_key = 0 AND i.is_unique_constraint = 0 AND i.type > 0
		ORDER BY i.name, ic.is_included_column, ic.key_ordinal, ic.index_column_id`, s, t.Name)
	if err != nil {
		return "", err
	}
	groups := groupRows(idx, 0)
	if len(groups) > 0 {
		b.WriteString("\n")
	}
	for _, g := range groups {
		var keyRows, incl [][]string
		for _, r := range g {
			if r[5] == "1" {
				incl = append(incl, r)
			} else {
				keyRows = append(keyRows, r)
			}
		}
		u := ""
		if g[0][1] == "1" {
			u = "UNIQUE "
		}
		fmt.Fprintf(&b, "\nCREATE %s%s INDEX %s ON %s (%s)", u, g[0][2], msq(g[0][0]), full, joinCols(keyRows, 3, 4))
		if len(incl) > 0 {
			fmt.Fprintf(&b, " INCLUDE (%s)", joinCols(incl, 3, -1))
		}
		if g[0][6] != "" {
			b.WriteString(" WHERE " + g[0][6])
		}
		b.WriteString(";")
	}
	return b.String(), nil
}

func mssqlType(name, maxLen, prec, scale string) string {
	switch name {
	case "varchar", "char", "varbinary", "binary":
		if maxLen == "-1" {
			return name + "(max)"
		}
		return name + "(" + maxLen + ")"
	case "nvarchar", "nchar":
		if maxLen == "-1" {
			return name + "(max)"
		}
		var n int
		fmt.Sscan(maxLen, &n)
		return fmt.Sprintf("%s(%d)", name, n/2)
	case "decimal", "numeric":
		return name + "(" + prec + "," + scale + ")"
	case "datetime2", "time", "datetimeoffset":
		return name + "(" + scale + ")"
	}
	return name
}

// groupRows groups consecutive rows sharing the value in column k.
func groupRows(rows [][]string, k int) [][][]string {
	var out [][][]string
	for _, r := range rows {
		if n := len(out); n > 0 && out[n-1][0][k] == r[k] {
			out[n-1] = append(out[n-1], r)
		} else {
			out = append(out, [][]string{r})
		}
	}
	return out
}

func joinCols(rows [][]string, nameCol, descCol int) string {
	var parts []string
	for _, r := range rows {
		p := msq(r[nameCol])
		if descCol >= 0 && r[descCol] == "1" {
			p += " DESC"
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, ", ")
}

func (mssqlDialect) transactionalDDL() bool { return true }

func nq(s string) string { return "N" + sqlQuote(s, false) }

func (mssqlDialect) alterColumn(ctx context.Context, q Querier, db string, t Table, target string, a ColumnAlter) ([]string, error) {
	d := diffOf(a)
	sch := t.Schema
	if sch == "" {
		sch = "dbo"
	}
	col := msq(a.Old.Name)
	var out []string
	// A default is a named constraint: it has to go before the column type
	// can change, and is added back afterwards.
	var dfName sql.NullString
	if d.typ || d.def || d.null {
		var err error
		if dfName, err = mssqlDefault(ctx, q, db, target, a.Old.Name); err != nil {
			return nil, err
		}
	}
	newDef := defaultOf(a.New.Default)
	redoDefault := dfName.Valid && (d.typ || d.def)
	if redoDefault {
		out = append(out, "ALTER TABLE "+target+" DROP CONSTRAINT "+msq(dfName.String))
	}
	if d.typ || d.null {
		null := " NULL"
		if !a.New.Nullable {
			null = " NOT NULL"
		}
		out = append(out, "ALTER TABLE "+target+" ALTER COLUMN "+col+" "+a.New.Type+null)
	}
	if newDef != "" && (redoDefault || (d.def && !dfName.Valid)) {
		out = append(out, "ALTER TABLE "+target+" ADD DEFAULT "+newDef+" FOR "+col)
	}
	if d.comment {
		args := "@name = N'MS_Description', @level0type = N'SCHEMA', @level0name = " + nq(sch) +
			", @level1type = N'TABLE', @level1name = " + nq(t.Name) + ", @level2type = N'COLUMN', @level2name = " + nq(a.Old.Name)
		proc := msq(db) + ".sys."
		switch {
		case a.New.Comment == "":
			out = append(out, "EXEC "+proc+"sp_dropextendedproperty "+args)
		case a.Old.Comment == "":
			out = append(out, "EXEC "+proc+"sp_addextendedproperty "+args+", @value = "+nq(a.New.Comment))
		default:
			out = append(out, "EXEC "+proc+"sp_updateextendedproperty "+args+", @value = "+nq(a.New.Comment))
		}
	}
	return out, nil
}

func (mssqlDialect) addColumn(db string, t Table, target string, c Column) []string {
	def := "ALTER TABLE " + target + " ADD " + msq(c.Name) + " " + c.Type
	if c.Nullable {
		def += " NULL"
	} else {
		def += " NOT NULL"
	}
	if c.Default != "" {
		def += " DEFAULT " + c.Default
	}
	out := []string{def}
	if c.Comment != "" {
		sch := t.Schema
		if sch == "" {
			sch = "dbo"
		}
		out = append(out, "EXEC "+msq(db)+".sys.sp_addextendedproperty @name = N'MS_Description', @level0type = N'SCHEMA', @level0name = "+nq(sch)+
			", @level1type = N'TABLE', @level1name = "+nq(t.Name)+", @level2type = N'COLUMN', @level2name = "+nq(c.Name)+", @value = "+nq(c.Comment))
	}
	return out
}

// mssqlDefault: the name of a column's default constraint, if it has one.
func mssqlDefault(ctx context.Context, q Querier, db, target, col string) (sql.NullString, error) {
	var name sql.NullString
	err := q.QueryRowContext(ctx, `SELECT dc.name FROM `+msq(db)+`.sys.default_constraints dc
		JOIN `+msq(db)+`.sys.columns c ON c.object_id = dc.parent_object_id AND c.column_id = dc.parent_column_id
		WHERE dc.parent_object_id = OBJECT_ID(@p1) AND c.name = @p2`, target, col).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return name, err
}

// A column with a default can only be dropped after its constraint.
func (mssqlDialect) dropColumn(ctx context.Context, q Querier, db string, _ Table, target, name string) ([]string, error) {
	df, err := mssqlDefault(ctx, q, db, target, name)
	if err != nil {
		return nil, err
	}
	var out []string
	if df.Valid {
		out = append(out, "ALTER TABLE "+target+" DROP CONSTRAINT "+msq(df.String))
	}
	return append(out, "ALTER TABLE "+target+" DROP COLUMN "+msq(name)), nil
}

func (mssqlDialect) tableComment(db string, t Table, target, comment string) []string {
	sch := t.Schema
	if sch == "" {
		sch = "dbo"
	}
	args := "@name = N'MS_Description', @level0type = N'SCHEMA', @level0name = " + nq(sch) + ", @level1type = N'TABLE', @level1name = " + nq(t.Name)
	proc := "EXEC " + msq(db) + ".sys."
	switch {
	case comment == "":
		return []string{proc + "sp_dropextendedproperty " + args}
	case t.Comment == "":
		return []string{proc + "sp_addextendedproperty " + args + ", @value = " + nq(comment)}
	}
	return []string{proc + "sp_updateextendedproperty " + args + ", @value = " + nq(comment)}
}

func (mssqlDialect) Routines(ctx context.Context, q Querier, db string) ([]Routine, error) {
	d := msq(db)
	return scanRoutines(ctx, q, `SELECT s.name, o.name,
		CASE WHEN o.type = 'P' THEN 'PROCEDURE' ELSE 'FUNCTION' END, '', ''
		FROM `+d+`.sys.objects o JOIN `+d+`.sys.schemas s ON s.schema_id = o.schema_id
		WHERE o.type IN ('P', 'FN', 'IF', 'TF') AND o.is_ms_shipped = 0
		ORDER BY 3 DESC, CASE WHEN s.name = 'dbo' THEN 0 ELSE 1 END, s.name, o.name`)
}

func (mssqlDialect) RoutineSource(ctx context.Context, conn *sql.Conn, db string, r Routine) (string, error) {
	d := msq(db)
	var src sql.NullString
	err := conn.QueryRowContext(ctx, `SELECT m.definition FROM `+d+`.sys.sql_modules m
		JOIN `+d+`.sys.objects o ON o.object_id = m.object_id
		JOIN `+d+`.sys.schemas s ON s.schema_id = o.schema_id
		WHERE s.name = @p1 AND o.name = @p2`, r.Schema, r.Name).Scan(&src)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(src.String) + "\nGO", nil
}

func (mssqlDialect) Param(n int, _ string) string {
	return fmt.Sprintf("@p%d", n)
}

// ---- indexes

func (mssqlDialect) listIndexes(ctx context.Context, q Querier, db string, t Table) ([]Index, error) {
	d := msq(db)
	s := t.Schema
	if s == "" {
		s = "dbo"
	}
	rows, err := rowStrings(ctx, q, `SELECT i.name, CAST(i.is_unique AS int), CAST(i.is_primary_key AS int),
		CASE WHEN i.is_primary_key = 1 OR i.is_unique_constraint = 1 THEN i.name ELSE '' END,
		i.type_desc, COALESCE(i.filter_definition, ''), CAST(ic.is_included_column AS int), col.name, CAST(ic.is_descending_key AS int)
		FROM `+d+`.sys.indexes i
		JOIN `+d+`.sys.objects o ON o.object_id = i.object_id JOIN `+d+`.sys.schemas sc ON sc.schema_id = o.schema_id
		JOIN `+d+`.sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
		JOIN `+d+`.sys.columns col ON col.object_id = ic.object_id AND col.column_id = ic.column_id
		WHERE o.name = @p2 AND sc.name = @p1 AND i.type > 0
		ORDER BY i.is_primary_key DESC, i.name, ic.is_included_column, ic.key_ordinal, ic.index_column_id`, s, t.Name)
	if err != nil {
		return nil, err
	}
	return groupIndexes(rows, func(r []string) Index {
		return Index{Name: r[0], Unique: r[1] == "1", Primary: r[2] == "1", Constraint: r[3], Type: r[4], Where: r[5]}
	}, func(ix *Index, r []string) {
		if r[6] == "1" {
			ix.Include = append(ix.Include, r[7])
		} else {
			ix.Columns = append(ix.Columns, colExpr(r[7], r[8] == "1"))
		}
	}), nil
}

func (d mssqlDialect) createIndex(db string, t Table, ix NewIndex) string {
	return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)", uniqueKw(ix.Unique), msq(ix.Name), d.QualifiedName(db, t), indexColumns(ix, msq))
}

func (d mssqlDialect) dropIndex(db string, t Table, ix Index) (string, error) {
	if ix.Constraint != "" {
		return fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", d.QualifiedName(db, t), msq(ix.Constraint)), nil
	}
	return fmt.Sprintf("DROP INDEX %s ON %s", msq(ix.Name), d.QualifiedName(db, t)), nil
}

// sp_rename takes the old name as one string (schema.table) and the new
// name bare; prefixed with the database it runs there.
func (mssqlDialect) renameTable(db string, t Table, name string) string {
	s := t.Schema
	if s == "" {
		s = "dbo"
	}
	lit := func(x string) string { return "N'" + strings.ReplaceAll(x, "'", "''") + "'" }
	return fmt.Sprintf("EXEC %s.sys.sp_rename %s, %s", msq(db), lit(msq(s)+"."+msq(t.Name)), lit(name))
}
