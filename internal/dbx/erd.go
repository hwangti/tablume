package dbx

import (
	"context"
	"sort"
	"strings"

	"tablume/internal/i18n"
)

// The ERD draws a schema's tables and the foreign keys between them. The
// frontend lays it out (it can measure text); this file gathers the data
// and reads each relation's cardinality off the keys, which is all the DDL
// can tell.

type GraphTable struct {
	Name    string   `json:"name"`
	View    bool     `json:"view"`
	Comment string   `json:"comment"`
	Columns []Column `json:"columns"` // Name, Type, Nullable, PK, Comment, Extra
}

type ForeignKey struct {
	Name       string   `json:"name"`
	Table      string   `json:"table"`
	Columns    []string `json:"columns"`
	RefSchema  string   `json:"refSchema"` // "" = this schema
	RefTable   string   `json:"refTable"`
	RefColumns []string `json:"refColumns"`
	OnUpdate   string   `json:"onUpdate"`
	OnDelete   string   `json:"onDelete"`
	// One: the child's FK columns are unique, so a parent has at most one
	// child (1:1); otherwise 1:N.
	One bool `json:"one"`
	// Required: every FK column is NOT NULL, so each child has a parent.
	Required bool `json:"required"`
	// LooseRef: the referenced columns aren't unique (InnoDB allows that),
	// so a child can match several parents: in effect N:M.
	LooseRef bool `json:"looseRef"`
}

type SchemaGraph struct {
	Tables []GraphTable `json:"tables"`
	FKs    []ForeignKey `json:"fks"`
}

// graphData is what a dialect reads. uniques holds the column sets of each
// table's unique keys, the primary key included.
type graphData struct {
	tables  []GraphTable
	fks     []ForeignKey
	uniques map[string][][]string
}

type graphDialect interface {
	schemaGraph(ctx context.Context, q Querier, schema string) (graphData, error)
}

func (m *Manager) SchemaGraph(id, schema string) (SchemaGraph, error) {
	p, err := m.pool(id)
	if err != nil {
		return SchemaGraph{}, err
	}
	gd, ok := p.d.(graphDialect)
	if !ok {
		return SchemaGraph{}, i18n.Error("이 DB에서는 ERD를 볼 수 없습니다")
	}
	ctx, cancel := metaCtx()
	defer cancel()
	d, err := gd.schemaGraph(ctx, p.db, schema)
	if err != nil {
		return SchemaGraph{}, err
	}
	return buildGraph(d), nil
}

func buildGraph(d graphData) SchemaGraph {
	cols := map[string]map[string]Column{}
	for _, t := range d.tables {
		cols[t.Name] = map[string]Column{}
		for _, c := range t.Columns {
			cols[t.Name][c.Name] = c
		}
	}
	for i := range d.fks {
		fk := &d.fks[i]
		fk.One = uniqueCovers(d.uniques[fk.Table], fk.Columns)
		fk.Required = true
		for _, c := range fk.Columns {
			if cols[fk.Table][c].Nullable {
				fk.Required = false
			}
		}
		// A table in another schema: its keys weren't read, so no verdict.
		if _, ok := cols[fk.RefTable]; ok && fk.RefSchema == "" {
			fk.LooseRef = !uniqueCovers(d.uniques[fk.RefTable], fk.RefColumns)
		}
	}
	less := func(a, b string) bool {
		if la, lb := strings.ToLower(a), strings.ToLower(b); la != lb {
			return la < lb
		}
		return a < b
	}
	sort.SliceStable(d.tables, func(i, j int) bool { return less(d.tables[i].Name, d.tables[j].Name) })
	if d.tables == nil {
		d.tables = []GraphTable{}
	}
	if d.fks == nil {
		d.fks = []ForeignKey{}
	}
	return SchemaGraph{Tables: d.tables, FKs: d.fks}
}

// uniqueCovers: some unique key lies within cols. If part of a column set
// is unique, so is the whole set; a key that only starts with cols (UNIQUE
// (a, b) for an FK on a) doesn't make them unique.
func uniqueCovers(keys [][]string, cols []string) bool {
	in := map[string]bool{}
	for _, c := range cols {
		in[c] = true
	}
next:
	for _, k := range keys {
		for _, c := range k {
			if !in[c] {
				continue next
			}
		}
		return len(k) > 0
	}
	return false
}

func (mysqlDialect) schemaGraph(ctx context.Context, q Querier, schema string) (graphData, error) {
	d := graphData{uniques: map[string][][]string{}}
	rows, err := rowStrings(ctx, q, `SELECT TABLE_NAME, TABLE_TYPE = 'VIEW', CASE WHEN TABLE_TYPE = 'VIEW' THEN '' ELSE TABLE_COMMENT END
		FROM information_schema.TABLES WHERE TABLE_SCHEMA = ?`, schema)
	if err != nil {
		return d, err
	}
	at := map[string]int{}
	for _, r := range rows {
		at[r[0]] = len(d.tables)
		d.tables = append(d.tables, GraphTable{Name: r[0], View: r[1] == "1", Comment: r[2], Columns: []Column{}})
	}

	rows, err = rowStrings(ctx, q, `SELECT TABLE_NAME, COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE = 'YES', COLUMN_KEY = 'PRI', COLUMN_COMMENT,
		TRIM(REPLACE(EXTRA, 'DEFAULT_GENERATED', ''))
		FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME, ORDINAL_POSITION`, schema)
	if err != nil {
		return d, err
	}
	for _, r := range rows {
		if i, ok := at[r[0]]; ok {
			d.tables[i].Columns = append(d.tables[i].Columns, Column{Name: r[1], Type: r[2], Nullable: r[3] == "1",
				PK: r[4] == "1", Comment: r[5], Extra: r[6]})
		}
	}

	rows, err = rowStrings(ctx, q, `SELECT TABLE_NAME, INDEX_NAME, COLUMN_NAME FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ? AND NON_UNIQUE = 0 ORDER BY TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX`, schema)
	if err != nil {
		return d, err
	}
	last := ""
	for _, r := range rows {
		ks := d.uniques[r[0]]
		if k := r[0] + "\x00" + r[1]; k != last {
			ks, last = append(ks, nil), k
		}
		ks[len(ks)-1] = append(ks[len(ks)-1], r[2])
		d.uniques[r[0]] = ks
	}

	rows, err = rowStrings(ctx, q, `SELECT k.TABLE_NAME, k.CONSTRAINT_NAME, k.COLUMN_NAME,
		CASE WHEN k.REFERENCED_TABLE_SCHEMA = k.TABLE_SCHEMA THEN '' ELSE k.REFERENCED_TABLE_SCHEMA END,
		k.REFERENCED_TABLE_NAME, k.REFERENCED_COLUMN_NAME, r.UPDATE_RULE, r.DELETE_RULE
		FROM information_schema.KEY_COLUMN_USAGE k
		JOIN information_schema.REFERENTIAL_CONSTRAINTS r ON r.CONSTRAINT_SCHEMA = k.CONSTRAINT_SCHEMA
			AND r.CONSTRAINT_NAME = k.CONSTRAINT_NAME AND r.TABLE_NAME = k.TABLE_NAME
		WHERE k.TABLE_SCHEMA = ? AND k.REFERENCED_TABLE_NAME IS NOT NULL
		ORDER BY k.TABLE_NAME, k.CONSTRAINT_NAME, k.ORDINAL_POSITION`, schema)
	if err != nil {
		return d, err
	}
	for _, r := range rows {
		if n := len(d.fks); n == 0 || d.fks[n-1].Table != r[0] || d.fks[n-1].Name != r[1] {
			d.fks = append(d.fks, ForeignKey{Table: r[0], Name: r[1], RefSchema: r[3], RefTable: r[4], OnUpdate: r[6], OnDelete: r[7]})
		}
		fk := &d.fks[len(d.fks)-1]
		fk.Columns = append(fk.Columns, r[2])
		fk.RefColumns = append(fk.RefColumns, r[5])
	}
	return d, nil
}
