package dbx

import (
	"context"
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
