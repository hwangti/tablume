package dbx

import (
	"fmt"
	"strings"
	"tablume/internal/i18n"
)

// Making a table from the structure view: its name, comment, engine and
// columns (those marked PK make the primary key). Indexes and foreign keys
// are added afterwards, on the new table.

type CreateRequest struct {
	ConnID  string   `json:"connId"`
	Schema  string   `json:"schema"`
	Table   Table    `json:"table"` // Name, Comment; Schema on SQL Server
	Columns []Column `json:"columns"`
	Engine  string   `json:"engine"` // MySQL/MariaDB, "" = the server's default
}

// foldName: a plain name in the case the database gives an unquoted one
// (PostgreSQL lower, Oracle upper), so a typed "new_col" is not made a
// quoted, case-sensitive name.
func foldName(driver, name string) string {
	if !plainIdent.MatchString(name) {
		return name
	}
	switch driver {
	case "postgres":
		return strings.ToLower(name)
	case "oracle":
		return strings.ToUpper(name)
	}
	return name
}

func (m *Manager) createPlan(req CreateRequest) (*pool, Table, []string, error) {
	p, err := m.pool(req.ConnID)
	if err != nil {
		return nil, Table{}, nil, err
	}
	ad, ok := p.d.(alterDialect)
	if !ok {
		return nil, Table{}, nil, i18n.Error("이 DB에서는 여기서 테이블을 만들 수 없습니다")
	}
	t := req.Table
	t.Name = foldName(p.cfg.Driver, strings.TrimSpace(t.Name))
	t.Type = "TABLE"
	if t.Name == "" {
		return nil, Table{}, nil, i18n.Error("테이블 이름이 비어 있습니다")
	}
	if len(req.Columns) == 0 {
		return nil, Table{}, nil, i18n.Error("컬럼을 하나 이상 넣으세요")
	}
	if p.cfg.Driver == "sqlite" {
		if err := sqliteCheck(t.Comment, req.Columns, true); err != nil {
			return nil, Table{}, nil, err
		}
	}
	target := p.d.QualifiedName(req.Schema, t)
	var defs, after, pk []string
	inlinePK := false // SQLite's auto increment says PRIMARY KEY itself
	seen := map[string]bool{}
	for _, c := range req.Columns {
		c.Name, c.Type, c.Default = strings.TrimSpace(c.Name), strings.TrimSpace(c.Type), defaultOf(c.Default)
		if c.Name == "" {
			return nil, Table{}, nil, i18n.Error("새 컬럼의 이름이 비어 있습니다")
		}
		if c.Type == "" {
			return nil, Table{}, nil, i18n.Errorf("%s: 타입이 비어 있습니다", c.Name)
		}
		if k := strings.ToLower(c.Name); seen[k] {
			return nil, Table{}, nil, i18n.Errorf("같은 이름의 컬럼이 이미 있습니다: %s", c.Name)
		} else {
			seen[k] = true
		}
		def, a := ad.columnDef(req.Schema, t, target, c)
		inlinePK = inlinePK || (p.cfg.Driver == "sqlite" && autoInc(c))
		defs = append(defs, def)
		after = append(after, a...)
		if c.PK {
			pk = append(pk, p.quote(foldName(p.cfg.Driver, c.Name)))
		}
	}
	if len(pk) > 0 && !inlinePK {
		defs = append(defs, "PRIMARY KEY ("+strings.Join(pk, ", ")+")")
	}
	st := "CREATE TABLE " + target + " (\n  " + strings.Join(defs, ",\n  ") + "\n)"
	if e := strings.TrimSpace(req.Engine); e != "" {
		if _, ok := p.d.(engineDialect); !ok || !engineName.MatchString(e) {
			return nil, Table{}, nil, i18n.Errorf("엔진을 바꿀 수 없습니다: %s", e)
		}
		st += " ENGINE = " + e
	}
	out := append([]string{st}, after...)
	if c := strings.TrimSpace(t.Comment); c != "" {
		made := t
		made.Comment = "" // (SQL Server: add the property, not update it)
		out = append(out, ad.tableComment(req.Schema, made, target, c)...)
	}
	return p, t, out, nil
}

// PreviewCreateTable returns the statements CreateTable would run.
func (m *Manager) PreviewCreateTable(req CreateRequest) ([]string, error) {
	_, _, stmts, err := m.createPlan(req)
	return stmts, err
}

type CreateResult struct {
	Table  Table       `json:"table"` // as made (the name folded where the database folds it)
	Result AlterResult `json:"result"`
}

// CreateTable makes the table.
func (m *Manager) CreateTable(req CreateRequest) (CreateResult, error) {
	p, t, stmts, err := m.createPlan(req)
	if err != nil {
		return CreateResult{}, err
	}
	r, err := runDDL(p, req.Schema, stmts)
	if err != nil {
		return CreateResult{Table: t}, fmt.Errorf("%s: %w", t.Name, err)
	}
	return CreateResult{Table: t, Result: r}, nil
}
