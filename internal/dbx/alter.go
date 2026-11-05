package dbx

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"tablume/internal/i18n"
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
	// Add: new columns, appended after the existing ones (Name, Type,
	// Nullable, Default, Comment).
	Add []Column `json:"add"`
	// Order: when set, every column name (existing and new) in the wanted
	// order, and Changes lists every existing column in its current order
	// (Old = New where only the place changes). MySQL/MariaDB only.
	Order []string `json:"order"`
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
	addColumn(schema string, t Table, target string, c Column) []string
	// transactionalDDL: the statements can run in one transaction.
	transactionalDDL() bool
}

// columnPlacer: a database whose ADD / MODIFY COLUMN take a position
// (MySQL, MariaDB). Its addColumn and alterColumn give one statement, which
// the position is appended to.
type columnPlacer interface {
	placeAfter(after string) string // "" = first
}

type placement struct{ name, after string }

// placements: the columns to put in place, in turn, each right after the
// one before it in order ("" = first), to turn orig into order. Columns of
// a longest run already in their original order stay; new ones (not in
// orig) and the rest are placed.
func placements(orig, order []string) []placement {
	pos := map[string]int{}
	for i, n := range orig {
		pos[n] = i
	}
	var names []string
	var idx []int
	for _, n := range order {
		if i, ok := pos[n]; ok {
			names, idx = append(names, n), append(idx, i)
		}
	}
	// ponytail: O(n²) longest increasing run; fine for a table's columns
	run, prev, best := make([]int, len(idx)), make([]int, len(idx)), -1
	for i := range idx {
		run[i], prev[i] = 1, -1
		for j := 0; j < i; j++ {
			if idx[j] < idx[i] && run[j]+1 > run[i] {
				run[i], prev[i] = run[j]+1, j
			}
		}
		if best < 0 || run[i] > run[best] {
			best = i
		}
	}
	stay := map[string]bool{}
	for i := best; i >= 0; i = prev[i] {
		stay[names[i]] = true
	}
	var out []placement
	for i, n := range order {
		if !stay[n] {
			after := ""
			if i > 0 {
				after = order[i-1]
			}
			out = append(out, placement{n, after})
		}
	}
	return out
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

// plainIdent: a name the database would take unquoted. Oracle and
// PostgreSQL fold those (upper / lower), so a typed "new_col" is given that
// case instead of becoming a quoted, case-sensitive name.
var plainIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*$`)

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
		return nil, nil, nil, i18n.Error("이 DB에서는 컬럼을 바꿀 수 없습니다 (SQLite는 테이블을 새로 만들어야 합니다)")
	}
	if strings.Contains(strings.ToUpper(req.Table.Type), "VIEW") {
		return nil, nil, nil, i18n.Error("뷰의 컬럼은 바꿀 수 없습니다")
	}
	ctx, cancel := metaCtx()
	defer cancel()
	target := p.d.QualifiedName(req.Schema, req.Table)
	var out []string
	for _, a := range req.Changes {
		if strings.TrimSpace(a.New.Type) == "" {
			return nil, nil, nil, i18n.Errorf("%s: 타입이 비어 있습니다", a.Old.Name)
		}
	}
	for i, c := range req.Add {
		c.Name, c.Type, c.Default = strings.TrimSpace(c.Name), strings.TrimSpace(c.Type), defaultOf(c.Default)
		if c.Name == "" {
			return nil, nil, nil, i18n.Error("새 컬럼의 이름이 비어 있습니다")
		}
		if c.Type == "" {
			return nil, nil, nil, i18n.Errorf("%s: 타입이 비어 있습니다", c.Name)
		}
		req.Add[i] = c
	}
	alter := func(a ColumnAlter, place string) error {
		stmts, err := ad.alterColumn(ctx, p.db, req.Schema, req.Table, target, a)
		if err != nil {
			return fmt.Errorf("%s: %w", a.Old.Name, err)
		}
		if len(stmts) > 0 {
			stmts[0] += place
		}
		out = append(out, stmts...)
		return nil
	}
	if len(req.Order) == 0 {
		for _, a := range req.Changes {
			if diffOf(a).any() {
				if err := alter(a, ""); err != nil {
					return nil, nil, nil, err
				}
			}
		}
		for _, c := range req.Add {
			out = append(out, ad.addColumn(req.Schema, req.Table, target, c)...)
		}
		return p, ad, out, nil
	}

	pl, ok := p.d.(columnPlacer)
	if !ok {
		return nil, nil, nil, i18n.Error("이 DB에서는 컬럼 순서를 바꿀 수 없습니다")
	}
	old := map[string]ColumnAlter{}
	var orig []string
	for _, a := range req.Changes {
		old[a.Old.Name] = a
		orig = append(orig, a.Old.Name)
	}
	added := map[string]Column{}
	for _, c := range req.Add {
		added[c.Name] = c
	}
	seen := map[string]bool{}
	for _, n := range req.Order {
		_, isOld := old[n]
		_, isNew := added[n]
		if seen[n] || isOld == isNew {
			return nil, nil, nil, i18n.Errorf("컬럼 순서가 맞지 않습니다: %s", n)
		}
		seen[n] = true
	}
	if len(seen) != len(old)+len(added) {
		return nil, nil, nil, i18n.Error("컬럼 순서에 빠진 컬럼이 있습니다")
	}
	place := map[string]string{}
	for _, pm := range placements(orig, req.Order) {
		place[pm.name] = pl.placeAfter(pm.after)
	}
	// in the wanted order, so the column each one goes after is in place
	for _, n := range req.Order {
		if c, ok := added[n]; ok {
			stmts := ad.addColumn(req.Schema, req.Table, target, c)
			stmts[0] += place[n]
			out = append(out, stmts...)
		} else if a, pos := old[n], place[n]; pos != "" || diffOf(a).any() {
			if err := alter(a, pos); err != nil {
				return nil, nil, nil, err
			}
		}
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
	p, _, stmts, err := m.alterPlan(req)
	if err != nil {
		return AlterResult{}, err
	}
	return runDDL(p, req.Schema, stmts)
}

// runDDL runs DDL statements on one connection: in a transaction where
// the database supports that, otherwise one by one up to the first error.
func runDDL(p *pool, schema string, stmts []string) (AlterResult, error) {
	res := AlterResult{Total: len(stmts)}
	ctx, cancel := metaCtx()
	defer cancel()
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return res, err
	}
	defer conn.Close()
	if q := p.d.UseSchema(schema); q != "" {
		_, _ = conn.ExecContext(ctx, q)
	}
	if transactionalDDL(p.d) {
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
