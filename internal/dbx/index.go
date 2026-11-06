package dbx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"tablume/internal/i18n"
)

// Index management from the structure view: list a table's indexes,
// create new ones and drop existing ones. Like column changes, the
// statements are shown before they run.

type Index struct {
	Name string `json:"name"`
	// Schema is the index's own schema when it can differ from the
	// table's (Oracle); "" otherwise.
	Schema string `json:"schema"`
	// Columns are the key columns as shown: "a", "b DESC", "c(10)" or an
	// expression.
	Columns []string `json:"columns"`
	Include []string `json:"include"` // non-key columns (SQL Server, PostgreSQL)
	Unique  bool     `json:"unique"`
	Primary bool     `json:"primary"`
	// Constraint names the PRIMARY KEY / UNIQUE constraint the index backs
	// ("" = a plain index); dropping it drops the constraint.
	Constraint string `json:"constraint"`
	Type       string `json:"type"`  // BTREE, HASH, GIN, NONCLUSTERED, NORMAL ...
	Where      string `json:"where"` // partial / filtered index
}

type IndexColumn struct {
	Name string `json:"name"`
	Desc bool   `json:"desc"`
}

type NewIndex struct {
	Name    string        `json:"name"`
	Columns []IndexColumn `json:"columns"`
	Unique  bool          `json:"unique"`
}

type IndexRequest struct {
	ConnID string     `json:"connId"`
	Schema string     `json:"schema"`
	Table  Table      `json:"table"`
	Drop   []Index    `json:"drop"`
	Create []NewIndex `json:"create"`
}

type indexDialect interface {
	listIndexes(ctx context.Context, q Querier, schema string, t Table) ([]Index, error)
	createIndex(schema string, t Table, ix NewIndex) string
	dropIndex(schema string, t Table, ix Index) (string, error)
}

// ListIndexes returns a table's indexes, the primary key first.
func (m *Manager) ListIndexes(id, schema string, t Table) ([]Index, error) {
	p, err := m.pool(id)
	if err != nil {
		return nil, err
	}
	xd, ok := p.d.(indexDialect)
	if !ok {
		return nil, i18n.Error("이 DB에서는 인덱스 목록을 볼 수 없습니다")
	}
	ctx, cancel := metaCtx()
	defer cancel()
	return xd.listIndexes(ctx, p.db, schema, t)
}

// indexCols groups rows of (index, ..., column) into indexes, keeping the
// order the query returned them in.
func groupIndexes(rows [][]string, make func(r []string) Index, add func(ix *Index, r []string)) []Index {
	var out []Index
	for _, r := range rows {
		if n := len(out); n == 0 || out[n-1].Name != r[0] {
			out = append(out, make(r))
		}
		add(&out[len(out)-1], r)
	}
	return out
}

// colExpr shows an index column, "(식)" when it is an expression with no
// name the catalog can give.
func colExpr(name string, desc bool) string {
	if name == "" {
		name = i18n.T("(식)")
	}
	if desc {
		name += " DESC"
	}
	return name
}

func indexColumns(ix NewIndex, quote func(string) string) string {
	parts := make([]string, len(ix.Columns))
	for i, c := range ix.Columns {
		parts[i] = quote(c.Name)
		if c.Desc {
			parts[i] += " DESC"
		}
	}
	return strings.Join(parts, ", ")
}

func uniqueKw(u bool) string {
	if u {
		return "UNIQUE "
	}
	return ""
}

func (m *Manager) indexPlan(req IndexRequest) (*pool, []string, error) {
	p, err := m.pool(req.ConnID)
	if err != nil {
		return nil, nil, err
	}
	xd, ok := p.d.(indexDialect)
	if !ok {
		return nil, nil, i18n.Error("이 DB에서는 인덱스를 바꿀 수 없습니다")
	}
	if strings.Contains(strings.ToUpper(req.Table.Type), "VIEW") {
		return nil, nil, i18n.Error("뷰에는 인덱스를 만들 수 없습니다")
	}
	var out []string
	for _, ix := range req.Drop {
		if ix.Primary {
			return nil, nil, i18n.Errorf("%s: 기본 키는 여기서 지울 수 없습니다", ix.Name)
		}
		st, err := xd.dropIndex(req.Schema, req.Table, ix)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", ix.Name, err)
		}
		out = append(out, st)
	}
	for _, ix := range req.Create {
		if strings.TrimSpace(ix.Name) == "" {
			return nil, nil, i18n.Error("인덱스 이름이 비어 있습니다")
		}
		if len(ix.Columns) == 0 {
			return nil, nil, i18n.Errorf("%s: 컬럼을 하나 이상 고르세요", ix.Name)
		}
		ix.Name = strings.TrimSpace(ix.Name)
		out = append(out, xd.createIndex(req.Schema, req.Table, ix))
	}
	return p, out, nil
}

// PreviewIndexes returns the statements ApplyIndexes would run.
func (m *Manager) PreviewIndexes(req IndexRequest) ([]string, error) {
	_, stmts, err := m.indexPlan(req)
	return stmts, err
}

// ApplyIndexes drops and creates indexes (drops first, so an index can be
// replaced under the same name).
func (m *Manager) ApplyIndexes(req IndexRequest) (AlterResult, error) {
	p, stmts, err := m.indexPlan(req)
	if err != nil {
		return AlterResult{}, err
	}
	return runDDL(p, req.Schema, stmts)
}

// transactionalDDL tells whether the database can undo DDL statements in
// a transaction (PostgreSQL, SQL Server, SQLite).
func transactionalDDL(d Dialect) bool {
	t, ok := d.(interface{ transactionalDDL() bool })
	return ok && t.transactionalDDL()
}

// ---- renaming tables

type renameDialect interface {
	renameTable(schema string, t Table, name string) string
}

// RenameTable renames a table or view and returns the statement it ran.
func (m *Manager) RenameTable(id, schema string, t Table, name string) (string, error) {
	p, err := m.pool(id)
	if err != nil {
		return "", err
	}
	rd, ok := p.d.(renameDialect)
	if !ok {
		return "", i18n.Error("이 DB에서는 이름을 바꿀 수 없습니다")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", i18n.Error("새 이름이 비어 있습니다")
	}
	if name == t.Name {
		return "", i18n.Error("이름이 같습니다")
	}
	return runOne(p, schema, rd.renameTable(schema, t, name))
}

// ---- dropping and emptying tables

// DropTable drops a table or view and returns the statement it ran.
func (m *Manager) DropTable(id, schema string, t Table) (string, error) {
	p, err := m.pool(id)
	if err != nil {
		return "", err
	}
	kind := "TABLE"
	switch ty := strings.ToUpper(t.Type); {
	case ty == "MATERIALIZED VIEW", ty == "FOREIGN TABLE":
		kind = ty
	case strings.Contains(ty, "VIEW"):
		kind = "VIEW"
	}
	return runOne(p, schema, "DROP "+kind+" "+p.d.QualifiedName(schema, t))
}

// TruncateTable deletes every row of a table (SQLite has no TRUNCATE:
// DELETE without WHERE, which it does the same fast way) and returns the
// statement it ran.
func (m *Manager) TruncateTable(id, schema string, t Table) (string, error) {
	p, err := m.pool(id)
	if err != nil {
		return "", err
	}
	if strings.Contains(strings.ToUpper(t.Type), "VIEW") {
		return "", i18n.Error("뷰는 비울 수 없습니다")
	}
	st := "TRUNCATE TABLE " + p.d.QualifiedName(schema, t)
	if p.cfg.Driver == "sqlite" {
		st = "DELETE FROM " + p.d.QualifiedName(schema, t)
	}
	return runOne(p, schema, st)
}

func runOne(p *pool, schema, st string) (string, error) {
	r, err := runDDL(p, schema, []string{st})
	if err != nil {
		return st, err
	}
	if r.Error != "" {
		return st, errors.New(r.Error)
	}
	return st, nil
}
