//go:build !no_mysql

package dbx

// MySQL / MariaDB: connection string, metadata, DDL, column changes, routines
// and bind parameters. Left out of builds with the no_mysql tag.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"tablume/internal/i18n"

	_ "github.com/go-sql-driver/mysql"
)

func init() { register(mysqlDialect{}, "mysql", "mariadb") }

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
		CASE WHEN TABLE_TYPE = 'VIEW' THEN '' ELSE TABLE_COMMENT END, -1, -1
		FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME`, schema)
}

func (mysqlDialect) TableStats(ctx context.Context, q Querier, schema string) ([]Table, error) {
	return scanStats(ctx, q, `SELECT TABLE_NAME, TABLE_ROWS, DATA_LENGTH + INDEX_LENGTH
		FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE <> 'VIEW'`, schema)
}

func (mysqlDialect) ListColumns(ctx context.Context, q Querier, schema string, t Table) ([]Column, error) {
	return scanColumns(ctx, q, `SELECT c.COLUMN_NAME, c.COLUMN_TYPE, c.IS_NULLABLE, c.COLUMN_KEY = 'PRI', c.COLUMN_COMMENT,
		EXISTS (SELECT 1 FROM information_schema.STATISTICS s WHERE s.TABLE_SCHEMA = c.TABLE_SCHEMA AND s.TABLE_NAME = c.TABLE_NAME
			AND s.COLUMN_NAME = c.COLUMN_NAME AND s.NON_UNIQUE = 0 AND s.INDEX_NAME <> 'PRIMARY'),
		(SELECT CONCAT(IF(k.REFERENCED_TABLE_SCHEMA <> k.TABLE_SCHEMA, CONCAT(k.REFERENCED_TABLE_SCHEMA, '.'), ''), k.REFERENCED_TABLE_NAME, '.', k.REFERENCED_COLUMN_NAME) FROM information_schema.KEY_COLUMN_USAGE k
			WHERE k.TABLE_SCHEMA = c.TABLE_SCHEMA AND k.TABLE_NAME = c.TABLE_NAME AND k.COLUMN_NAME = c.COLUMN_NAME
			AND k.REFERENCED_TABLE_NAME IS NOT NULL LIMIT 1),
		-- defaults as SQL expressions: MariaDB already reports them that way;
		-- MySQL 8 gives literals bare (DEFAULT_GENERATED marks expressions)
		CASE WHEN c.COLUMN_DEFAULT IS NULL THEN NULL
			WHEN @@version LIKE '%MariaDB%' OR c.EXTRA LIKE '%DEFAULT_GENERATED%' THEN c.COLUMN_DEFAULT
			ELSE QUOTE(c.COLUMN_DEFAULT) END,
		TRIM(REPLACE(c.EXTRA, 'DEFAULT_GENERATED', ''))
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
func (mysqlDialect) Select(q, where, orderBy string, n, offset int) string {
	return selectWithLimit(q, where, orderBy, limitClause(n, offset))
}
func (mysqlDialect) Version(ctx context.Context, q Querier) string {
	return firstString(ctx, q, "SELECT VERSION()")
}

func (mysqlDialect) DDL(ctx context.Context, q *sql.Conn, schema string, t Table) (string, error) {
	rows, err := rowStrings(ctx, q, "SHOW CREATE TABLE "+mysqlDialect{}.QualifiedName(schema, t))
	if err != nil {
		return "", err
	}
	if len(rows) == 0 || len(rows[0]) < 2 {
		return "", nil
	}
	return rows[0][1] + ";", nil
}

func (mysqlDialect) transactionalDDL() bool { return false }

var mysqlStringType = []string{"CHAR", "VARCHAR", "TEXT", "TINYTEXT", "MEDIUMTEXT", "LONGTEXT", "ENUM", "SET"}

// MODIFY restates the whole column, so everything not being changed
// (character set, collation, AUTO_INCREMENT, ON UPDATE …) is carried over.
func (mysqlDialect) alterColumn(ctx context.Context, q Querier, schema string, t Table, target string, a ColumnAlter) ([]string, error) {
	var cs, coll, extra, gen sql.NullString
	err := q.QueryRowContext(ctx, `SELECT CHARACTER_SET_NAME, COLLATION_NAME, EXTRA, GENERATION_EXPRESSION
		FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND COLUMN_NAME = ?`,
		schema, t.Name, a.Old.Name).Scan(&cs, &coll, &extra, &gen)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(gen.String) != "" {
		return nil, i18n.Error("계산(generated) 컬럼은 여기서 바꿀 수 없습니다")
	}
	n := a.New
	def := quoteWith(n.Name, "`", "`") + " " + strings.TrimSpace(n.Type)
	base, _, _ := strings.Cut(strings.ToUpper(strings.TrimSpace(n.Type)), "(")
	for _, st := range mysqlStringType {
		switch {
		case base != st:
		case a.Old.Collation != n.Collation && n.Collation != "": // the character set follows the collation
			def += " COLLATE " + n.Collation
		case cs.String != "":
			def += " CHARACTER SET " + cs.String + " COLLATE " + coll.String
		}
	}
	if n.Nullable {
		def += " NULL"
	} else {
		def += " NOT NULL"
	}
	if d := defaultOf(n.Default); d != "" {
		def += " DEFAULT " + d
	}
	ex := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(extra.String, "DEFAULT_GENERATED", "")))
	// INVISIBLE (MySQL 8.0.23+, MariaDB 10.3+): as wanted, or kept
	if a.Old.Invisible != n.Invisible && n.Invisible || a.Old.Invisible == n.Invisible && strings.Contains(ex, "invisible") {
		def += " INVISIBLE"
	}
	if autoInc(n) { // as wanted: on, off or kept
		def += " AUTO_INCREMENT"
	}
	if a.Old.OnUpdate != n.OnUpdate { // as wanted, "" = none
		if u := strings.TrimSpace(n.OnUpdate); u != "" {
			def += " ON UPDATE " + u
		}
	} else if u := onUpdateOf(ex); u != "" {
		def += " ON UPDATE " + u
	}
	if n.Comment != "" {
		def += " COMMENT " + sqlQuote(n.Comment, true)
	}
	if a.Old.Name != n.Name { // CHANGE: renamed, the definition restated
		return []string{"ALTER TABLE " + target + " CHANGE COLUMN " + quoteWith(a.Old.Name, "`", "`") + " " + def}, nil
	}
	return []string{"ALTER TABLE " + target + " MODIFY COLUMN " + def}, nil
}

func (mysqlDialect) placeAfter(after string) string {
	if after == "" {
		return " FIRST"
	}
	return " AFTER " + quoteWith(after, "`", "`")
}

func (mysqlDialect) tableEngine(ctx context.Context, q Querier, schema string, t Table) (TableOptions, error) {
	var o TableOptions
	var e sql.NullString
	// (a table not made yet: no engine, but the lists)
	if err := q.QueryRowContext(ctx, `SELECT ENGINE FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`, schema, t.Name).Scan(&e); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return o, err
	}
	o.Engine = e.String
	rows, err := rowStrings(ctx, q, `SELECT ENGINE FROM information_schema.ENGINES WHERE SUPPORT IN ('YES', 'DEFAULT') ORDER BY ENGINE`)
	if err != nil {
		return o, err
	}
	for _, r := range rows {
		o.Engines = append(o.Engines, r[0])
	}
	cols, err := rowStrings(ctx, q, `SELECT COLUMN_NAME, COALESCE(COLLATION_NAME, ''), EXTRA FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`, schema, t.Name)
	if err != nil {
		return o, err
	}
	o.Columns = map[string]ColumnOptions{}
	for _, r := range cols {
		o.Columns[r[0]] = ColumnOptions{Collation: r[1], OnUpdate: onUpdateOf(r[2]), Invisible: strings.Contains(strings.ToLower(r[2]), "invisible")}
	}
	colls, err := rowStrings(ctx, q, `SELECT COLLATION_NAME FROM information_schema.COLLATIONS ORDER BY COLLATION_NAME`)
	if err != nil {
		return o, err
	}
	for _, r := range colls {
		o.Collations = append(o.Collations, r[0])
	}
	return o, nil
}

// onUpdateOf: the ON UPDATE expression in a column's EXTRA ("" = none).
func onUpdateOf(extra string) string {
	ex := strings.ToLower(extra)
	if i := strings.Index(ex, "on update "); i >= 0 {
		return strings.TrimSpace(extra[i+len("on update "):])
	}
	return ""
}

func (d mysqlDialect) addColumn(schema string, t Table, target string, c Column) []string {
	def, after := d.columnDef(schema, t, target, c)
	return append([]string{"ALTER TABLE " + target + " ADD COLUMN " + def}, after...)
}

// columnDef: a new column's definition, for ADD COLUMN and CREATE TABLE.
func (mysqlDialect) columnDef(_ string, _ Table, _ string, c Column) (string, []string) {
	def := quoteWith(c.Name, "`", "`") + " " + c.Type
	if c.Collation != "" {
		def += " COLLATE " + c.Collation
	}
	if !c.Nullable {
		def += " NOT NULL"
	}
	if c.Default != "" {
		def += " DEFAULT " + c.Default
	}
	if c.Invisible {
		def += " INVISIBLE"
	}
	if autoInc(c) {
		def += " AUTO_INCREMENT"
	}
	if u := strings.TrimSpace(c.OnUpdate); u != "" {
		def += " ON UPDATE " + u
	}
	if c.Comment != "" {
		def += " COMMENT " + sqlQuote(c.Comment, true)
	}
	return def, nil
}

func (mysqlDialect) dropColumn(_ context.Context, _ Querier, _ string, _ Table, target, name string) ([]string, error) {
	return []string{"ALTER TABLE " + target + " DROP COLUMN " + quoteWith(name, "`", "`")}, nil
}

func (mysqlDialect) tableComment(_ string, _ Table, target, comment string) []string {
	return []string{"ALTER TABLE " + target + " COMMENT = " + sqlQuote(comment, true)}
}

func (mysqlDialect) Routines(ctx context.Context, q Querier, schema string) ([]Routine, error) {
	return scanRoutines(ctx, q, `SELECT ROUTINE_SCHEMA, ROUTINE_NAME, ROUTINE_TYPE, '', ''
		FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA = ? ORDER BY ROUTINE_TYPE DESC, ROUTINE_NAME`, schema)
}

func (mysqlDialect) RoutineSource(ctx context.Context, conn *sql.Conn, schema string, r Routine) (string, error) {
	kind := "PROCEDURE"
	if r.Type == "FUNCTION" {
		kind = "FUNCTION"
	}
	rows, err := rowStrings(ctx, conn, "SHOW CREATE "+kind+" "+quoteWith(schema, "`", "`")+"."+quoteWith(r.Name, "`", "`"))
	if err != nil {
		return "", err
	}
	if len(rows) == 0 || len(rows[0]) < 3 {
		return "", nil
	}
	// columns: name, sql_mode, Create Procedure/Function, …
	// The body contains ';' — wrap it so the console runs it as one statement.
	return "DELIMITER $$\n" + strings.TrimSpace(rows[0][2]) + "$$\nDELIMITER ;", nil
}

func (mysqlDialect) Param(int, string) string { return "?" }

// ---- indexes

func (mysqlDialect) listIndexes(ctx context.Context, q Querier, schema string, t Table) ([]Index, error) {
	rows, err := rowStrings(ctx, q, `SELECT INDEX_NAME, NON_UNIQUE, INDEX_TYPE, COLUMN_NAME, COALESCE(SUB_PART, ''), COALESCE(COLLATION, '')
		FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY INDEX_NAME <> 'PRIMARY', INDEX_NAME, SEQ_IN_INDEX`, schema, t.Name)
	if err != nil {
		return nil, err
	}
	return groupIndexes(rows, func(r []string) Index {
		ix := Index{Name: r[0], Unique: r[1] == "0", Primary: r[0] == "PRIMARY", Type: r[2]}
		if ix.Primary {
			ix.Constraint = "PRIMARY KEY"
		}
		return ix
	}, func(ix *Index, r []string) {
		c := r[3]
		if c != "" && r[4] != "" {
			c += "(" + r[4] + ")"
		}
		ix.Columns = append(ix.Columns, colExpr(c, r[5] == "D"))
	}), nil
}

func (d mysqlDialect) createIndex(schema string, t Table, ix NewIndex) string {
	quote := func(s string) string { return quoteWith(s, "`", "`") }
	return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)", uniqueKw(ix.Unique), quote(ix.Name), d.QualifiedName(schema, t), indexColumns(ix, quote))
}

func (d mysqlDialect) dropIndex(schema string, t Table, ix Index) (string, error) {
	return fmt.Sprintf("DROP INDEX %s ON %s", quoteWith(ix.Name, "`", "`"), d.QualifiedName(schema, t)), nil
}

func (d mysqlDialect) renameTable(schema string, t Table, name string) string {
	return fmt.Sprintf("RENAME TABLE %s TO %s.%s", d.QualifiedName(schema, t), quoteWith(schema, "`", "`"), quoteWith(name, "`", "`"))
}
