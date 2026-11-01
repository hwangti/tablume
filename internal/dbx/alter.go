package dbx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Editing a table's columns from the structure view: type, NULL, default
// and comment. Each dialect turns the changes into ALTER statements, shown
// to the user before they run.

// ColumnAlter is one column's change. Old is the column as shown when the
// edit began; New carries the wanted Type, Nullable, Default (an SQL
// expression, "" = none) and Comment.
type ColumnAlter struct {
	Old Column `json:"old"`
	New Column `json:"new"`
}

type AlterRequest struct {
	ConnID  string        `json:"connId"`
	Schema  string        `json:"schema"`
	Table   Table         `json:"table"`
	Changes []ColumnAlter `json:"changes"`
}

type AlterResult struct {
	Applied int    `json:"applied"` // statements that ran
	Total   int    `json:"total"`
	Error   string `json:"error"`
	// RolledBack: the database undid the applied statements too
	// (transactional DDL: PostgreSQL, SQL Server).
	RolledBack bool `json:"rolledBack"`
}

type alterDialect interface {
	alterColumn(ctx context.Context, q Querier, schema string, t Table, target string, a ColumnAlter) ([]string, error)
	// transactionalDDL: the statements can run in one transaction.
	transactionalDDL() bool
}

func defaultOf(s string) string {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "NULL") {
		return ""
	}
	return s
}

type colDiff struct{ typ, null, def, comment bool }

func diffOf(a ColumnAlter) colDiff {
	return colDiff{
		typ:     !strings.EqualFold(strings.TrimSpace(a.Old.Type), strings.TrimSpace(a.New.Type)),
		null:    a.Old.Nullable != a.New.Nullable,
		def:     defaultOf(a.Old.Default) != defaultOf(a.New.Default),
		comment: a.Old.Comment != a.New.Comment,
	}
}

func (d colDiff) any() bool { return d.typ || d.null || d.def || d.comment }

func sqlQuote(s string, backslash bool) string {
	if backslash {
		s = strings.ReplaceAll(s, `\`, `\\`)
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func (m *Manager) alterPlan(req AlterRequest) (*pool, alterDialect, []string, error) {
	p, err := m.pool(req.ConnID)
	if err != nil {
		return nil, nil, nil, err
	}
	ad, ok := p.d.(alterDialect)
	if !ok {
		return nil, nil, nil, errors.New("이 DB에서는 컬럼을 바꿀 수 없습니다 (SQLite는 테이블을 새로 만들어야 합니다)")
	}
	if strings.Contains(strings.ToUpper(req.Table.Type), "VIEW") {
		return nil, nil, nil, errors.New("뷰의 컬럼은 바꿀 수 없습니다")
	}
	ctx, cancel := metaCtx()
	defer cancel()
	target := p.d.QualifiedName(req.Schema, req.Table)
	var out []string
	for _, a := range req.Changes {
		if strings.TrimSpace(a.New.Type) == "" {
			return nil, nil, nil, fmt.Errorf("%s: 타입이 비어 있습니다", a.Old.Name)
		}
		if !diffOf(a).any() {
			continue
		}
		stmts, err := ad.alterColumn(ctx, p.db, req.Schema, req.Table, target, a)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", a.Old.Name, err)
		}
		out = append(out, stmts...)
	}
	return p, ad, out, nil
}

// PreviewAlter returns the statements ApplyAlter would run.
func (m *Manager) PreviewAlter(req AlterRequest) ([]string, error) {
	_, _, stmts, err := m.alterPlan(req)
	return stmts, err
}

// ApplyAlter runs the statements: in one transaction where DDL is
// transactional, otherwise one by one, stopping at the first error.
func (m *Manager) ApplyAlter(req AlterRequest) (AlterResult, error) {
	p, ad, stmts, err := m.alterPlan(req)
	if err != nil {
		return AlterResult{}, err
	}
	res := AlterResult{Total: len(stmts)}
	ctx, cancel := metaCtx()
	defer cancel()
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return res, err
	}
	defer conn.Close()
	if q := p.d.UseSchema(req.Schema); q != "" {
		_, _ = conn.ExecContext(ctx, q)
	}
	if ad.transactionalDDL() {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return res, err
		}
		for _, st := range stmts {
			if _, err := tx.ExecContext(ctx, st); err != nil {
				_ = tx.Rollback()
				res.Error = err.Error()
				res.RolledBack = true
				return res, nil
			}
			res.Applied++
		}
		if err := tx.Commit(); err != nil {
			res.Error = err.Error()
			res.RolledBack = true
			res.Applied = 0
		}
		return res, nil
	}
	for _, st := range stmts {
		if _, err := conn.ExecContext(ctx, st); err != nil {
			res.Error = err.Error()
			return res, nil
		}
		res.Applied++
	}
	return res, nil
}

// ---------------------------------------------------------------- MySQL

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
		return nil, errors.New("계산(generated) 컬럼은 여기서 바꿀 수 없습니다")
	}
	n := a.New
	def := quoteWith(n.Name, "`", "`") + " " + strings.TrimSpace(n.Type)
	base, _, _ := strings.Cut(strings.ToUpper(strings.TrimSpace(n.Type)), "(")
	for _, st := range mysqlStringType {
		if base == st && cs.String != "" {
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
	if strings.Contains(ex, "auto_increment") {
		def += " AUTO_INCREMENT"
	}
	if i := strings.Index(ex, "on update "); i >= 0 {
		def += " ON UPDATE " + strings.TrimSpace(ex[i+len("on update "):])
	}
	if n.Comment != "" {
		def += " COMMENT " + sqlQuote(n.Comment, true)
	}
	return []string{"ALTER TABLE " + target + " MODIFY COLUMN " + def}, nil
}

// ---------------------------------------------------------------- PostgreSQL

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
	if d.def {
		if v := defaultOf(a.New.Default); v != "" {
			out = append(out, head+" SET DEFAULT "+v)
		} else {
			out = append(out, head+" DROP DEFAULT")
		}
	}
	if d.comment {
		c := "NULL"
		if a.New.Comment != "" {
			c = sqlQuote(a.New.Comment, false)
		}
		out = append(out, "COMMENT ON COLUMN "+target+"."+col+" IS "+c)
	}
	return out, nil
}

// ---------------------------------------------------------------- SQL Server

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
		err := q.QueryRowContext(ctx, `SELECT dc.name FROM `+msq(db)+`.sys.default_constraints dc
			JOIN `+msq(db)+`.sys.columns c ON c.object_id = dc.parent_object_id AND c.column_id = dc.parent_column_id
			WHERE dc.parent_object_id = OBJECT_ID(@p1) AND c.name = @p2`, target, a.Old.Name).Scan(&dfName)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
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

// ---------------------------------------------------------------- Oracle

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
