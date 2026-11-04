package dbx

import (
	"context"
	"database/sql"
	"strings"
	"tablume/internal/i18n"
)

// DDL returns the CREATE statement(s) for a table or view.
func (m *Manager) DDL(id, schema string, t Table) (string, error) {
	p, err := m.pool(id)
	if err != nil {
		return "", err
	}
	ctx, cancel := metaCtx()
	defer cancel()
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	ddl, err := p.d.DDL(ctx, conn, schema, t)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(ddl) == "" {
		return "", i18n.Errorf("%s의 DDL을 찾을 수 없습니다", t.Name)
	}
	return strings.TrimSpace(ddl) + "\n", nil
}

// rowStrings scans every row into string slices (NULL -> "").
func rowStrings(ctx context.Context, q Querier, query string, args ...any) ([][]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out [][]string
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = v.String
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
