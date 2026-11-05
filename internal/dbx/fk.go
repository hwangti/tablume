package dbx

import (
	"context"
	"strings"
	"tablume/internal/i18n"
)

// Foreign keys from the structure view: list a table's foreign keys, add
// new ones and drop existing ones. The statements are shown before they
// run, as with columns and indexes.

type TableFK struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	// RefTable is the referenced table; its Schema is set (SQL Server) or
	// RefSchema names another schema.
	RefTable   Table    `json:"refTable"`
	RefSchema  string   `json:"refSchema"` // "" = the same schema
	RefColumns []string `json:"refColumns"`
	OnUpdate   string   `json:"onUpdate"` // NO ACTION, CASCADE, SET NULL, ...; "" = not supported (Oracle)
	OnDelete   string   `json:"onDelete"`
}

type FKRequest struct {
	ConnID string    `json:"connId"`
	Schema string    `json:"schema"`
	Table  Table     `json:"table"`
	Drop   []TableFK `json:"drop"`
	Create []TableFK `json:"create"`
}

type fkDialect interface {
	// rows: name, column, ref schema, ref table, ref column, on update, on delete
	listFKs(ctx context.Context, q Querier, schema string, t Table) ([][]string, error)
}

// ListForeignKeys returns a table's foreign keys by name.
func (m *Manager) ListForeignKeys(id, schema string, t Table) ([]TableFK, error) {
	p, err := m.pool(id)
	if err != nil {
		return nil, err
	}
	fd, ok := p.d.(fkDialect)
	if !ok {
		return nil, i18n.Error("이 DB에서는 외래 키를 볼 수 없습니다")
	}
	ctx, cancel := metaCtx()
	defer cancel()
	rows, err := fd.listFKs(ctx, p.db, schema, t)
	if err != nil {
		return nil, err
	}
	own := schema
	if p.cfg.Driver == "mssql" {
		own = t.Schema
		if own == "" {
			own = "dbo"
		}
	}
	out := []TableFK{}
	for _, r := range rows {
		if n := len(out); n == 0 || out[n-1].Name != r[0] {
			fk := TableFK{Name: r[0], RefTable: Table{Name: r[3], Type: "TABLE"}, OnUpdate: rule(r[5]), OnDelete: rule(r[6])}
			if p.cfg.Driver == "mssql" {
				fk.RefTable.Schema = r[2]
			}
			if !strings.EqualFold(r[2], own) {
				fk.RefSchema = r[2]
			}
			out = append(out, fk)
		}
		fk := &out[len(out)-1]
		fk.Columns = append(fk.Columns, r[1])
		fk.RefColumns = append(fk.RefColumns, r[4])
	}
	return out, nil
}

// rule: the catalogs' spellings (NO_ACTION, SET_NULL, PostgreSQL's
// one-letter codes) as SQL.
func rule(s string) string {
	switch s {
	case "a":
		return "NO ACTION"
	case "r":
		return "RESTRICT"
	case "c":
		return "CASCADE"
	case "n":
		return "SET NULL"
	case "d":
		return "SET DEFAULT"
	}
	return strings.ReplaceAll(strings.ToUpper(s), "_", " ")
}

func (m *Manager) fkPlan(req FKRequest) (*pool, []string, error) {
	p, err := m.pool(req.ConnID)
	if err != nil {
		return nil, nil, err
	}
	if _, ok := p.d.(fkDialect); !ok {
		return nil, nil, i18n.Error("이 DB에서는 외래 키를 바꿀 수 없습니다")
	}
	target := p.d.QualifiedName(req.Schema, req.Table)
	var out []string
	for _, fk := range req.Drop {
		kw := " DROP CONSTRAINT "
		if p.cfg.Driver == "mysql" || p.cfg.Driver == "mariadb" {
			kw = " DROP FOREIGN KEY "
		}
		out = append(out, "ALTER TABLE "+target+kw+p.quote(fk.Name))
	}
	for _, fk := range req.Create {
		fk.Name = strings.TrimSpace(fk.Name)
		if fk.Name == "" {
			return nil, nil, i18n.Error("외래 키 이름이 비어 있습니다")
		}
		if len(fk.Columns) == 0 || len(fk.Columns) != len(fk.RefColumns) || fk.RefTable.Name == "" {
			return nil, nil, i18n.Errorf("%s: 컬럼과 참조할 컬럼을 짝지어 고르세요", fk.Name)
		}
		q := func(cs []string) string {
			parts := make([]string, len(cs))
			for i, c := range cs {
				parts[i] = p.quote(c)
			}
			return strings.Join(parts, ", ")
		}
		refSchema := req.Schema
		if fk.RefSchema != "" && p.cfg.Driver != "mssql" { // SQL Server: in RefTable.Schema
			refSchema = fk.RefSchema
		}
		st := "ALTER TABLE " + target + " ADD CONSTRAINT " + p.quote(fk.Name) +
			" FOREIGN KEY (" + q(fk.Columns) + ") REFERENCES " + p.d.QualifiedName(refSchema, fk.RefTable) + " (" + q(fk.RefColumns) + ")"
		// NO ACTION is every database's default: left out
		if r := strings.ToUpper(strings.TrimSpace(fk.OnDelete)); r != "" && r != "NO ACTION" {
			st += " ON DELETE " + r
		}
		if r := strings.ToUpper(strings.TrimSpace(fk.OnUpdate)); r != "" && r != "NO ACTION" {
			st += " ON UPDATE " + r
		}
		out = append(out, st)
	}
	return p, out, nil
}

// PreviewForeignKeys returns the statements ApplyForeignKeys would run.
func (m *Manager) PreviewForeignKeys(req FKRequest) ([]string, error) {
	_, stmts, err := m.fkPlan(req)
	return stmts, err
}

// ApplyForeignKeys drops, then adds foreign keys.
func (m *Manager) ApplyForeignKeys(req FKRequest) (AlterResult, error) {
	p, stmts, err := m.fkPlan(req)
	if err != nil {
		return AlterResult{}, err
	}
	return runDDL(p, req.Schema, stmts)
}

// ---- catalogs

func (mysqlDialect) listFKs(ctx context.Context, q Querier, schema string, t Table) ([][]string, error) {
	return rowStrings(ctx, q, `SELECT k.CONSTRAINT_NAME, k.COLUMN_NAME, k.REFERENCED_TABLE_SCHEMA, k.REFERENCED_TABLE_NAME,
		k.REFERENCED_COLUMN_NAME, r.UPDATE_RULE, r.DELETE_RULE
		FROM information_schema.KEY_COLUMN_USAGE k
		JOIN information_schema.REFERENTIAL_CONSTRAINTS r ON r.CONSTRAINT_SCHEMA = k.CONSTRAINT_SCHEMA
			AND r.CONSTRAINT_NAME = k.CONSTRAINT_NAME AND r.TABLE_NAME = k.TABLE_NAME
		WHERE k.TABLE_SCHEMA = ? AND k.TABLE_NAME = ? AND k.REFERENCED_TABLE_NAME IS NOT NULL
		ORDER BY k.CONSTRAINT_NAME, k.ORDINAL_POSITION`, schema, t.Name)
}

func (pgDialect) listFKs(ctx context.Context, q Querier, schema string, t Table) ([][]string, error) {
	return rowStrings(ctx, q, `SELECT c.conname, a.attname, rn.nspname, rc.relname, ra.attname, c.confupdtype, c.confdeltype
		FROM pg_constraint c
		JOIN pg_class tc ON tc.oid = c.conrelid JOIN pg_namespace n ON n.oid = tc.relnamespace
		JOIN pg_class rc ON rc.oid = c.confrelid JOIN pg_namespace rn ON rn.oid = rc.relnamespace
		CROSS JOIN LATERAL unnest(c.conkey, c.confkey) WITH ORDINALITY AS k(col, refcol, i)
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.col
		JOIN pg_attribute ra ON ra.attrelid = c.confrelid AND ra.attnum = k.refcol
		WHERE c.contype = 'f' AND n.nspname = $1 AND tc.relname = $2
		ORDER BY c.conname, k.i`, schema, t.Name)
}

// Oracle has no ON UPDATE rule: "" in that place.
func (oracleDialect) listFKs(ctx context.Context, q Querier, schema string, t Table) ([][]string, error) {
	return rowStrings(ctx, q, `SELECT c.constraint_name, cc.column_name, r.owner, r.table_name, rc.column_name, '', c.delete_rule
		FROM all_constraints c
		JOIN all_cons_columns cc ON cc.owner = c.owner AND cc.constraint_name = c.constraint_name
		JOIN all_constraints r ON r.owner = c.r_owner AND r.constraint_name = c.r_constraint_name
		JOIN all_cons_columns rc ON rc.owner = r.owner AND rc.constraint_name = r.constraint_name AND rc.position = cc.position
		WHERE c.constraint_type = 'R' AND c.owner = :1 AND c.table_name = :2
		ORDER BY c.constraint_name, cc.position`, schema, t.Name)
}

func (mssqlDialect) listFKs(ctx context.Context, q Querier, db string, t Table) ([][]string, error) {
	d := msq(db)
	return rowStrings(ctx, q, `SELECT fk.name, pc.name, rs.name, rt.name, rc.name,
		fk.update_referential_action_desc, fk.delete_referential_action_desc
		FROM `+d+`.sys.foreign_keys fk
		JOIN `+d+`.sys.foreign_key_columns fkc ON fkc.constraint_object_id = fk.object_id
		JOIN `+d+`.sys.columns pc ON pc.object_id = fkc.parent_object_id AND pc.column_id = fkc.parent_column_id
		JOIN `+d+`.sys.tables rt ON rt.object_id = fk.referenced_object_id
		JOIN `+d+`.sys.schemas rs ON rs.schema_id = rt.schema_id
		JOIN `+d+`.sys.columns rc ON rc.object_id = fkc.referenced_object_id AND rc.column_id = fkc.referenced_column_id
		WHERE fk.parent_object_id = OBJECT_ID(@p1)
		ORDER BY fk.name, fkc.constraint_column_id`, (mssqlDialect{}).QualifiedName(db, t))
}
