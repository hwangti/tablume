package dbx

import (
	"context"
	"database/sql"
	"strings"
	"tablume/internal/i18n"
)

// Routine is a stored procedure, function or (Oracle) package.
type Routine struct {
	Schema string `json:"schema"` // SQL Server: schema inside the database
	Name   string `json:"name"`
	Type   string `json:"type"` // PROCEDURE, FUNCTION, PACKAGE
	// Args tells overloads apart (PostgreSQL identity arguments).
	Args string `json:"args"`
	// ID identifies the routine for RoutineSource where the name isn't
	// enough (PostgreSQL function oid).
	ID string `json:"id"`
}

type routineDialect interface {
	Routines(ctx context.Context, q Querier, schema string) ([]Routine, error)
	RoutineSource(ctx context.Context, conn *sql.Conn, schema string, r Routine) (string, error)
}

func scanRoutines(ctx context.Context, q Querier, query string, args ...any) ([]Routine, error) {
	rows, err := rowStrings(ctx, q, query, args...)
	if err != nil {
		return nil, err
	}
	out := []Routine{}
	for _, r := range rows {
		// schema, name, type, args, id
		out = append(out, Routine{Schema: r[0], Name: r[1], Type: r[2], Args: r[3], ID: r[4]})
	}
	return out, nil
}

// Routines lists a schema's procedures and functions (empty for SQLite).
func (m *Manager) Routines(id, schema string) ([]Routine, error) {
	p, err := m.pool(id)
	if err != nil {
		return nil, err
	}
	rd, ok := p.d.(routineDialect)
	if !ok {
		return []Routine{}, nil
	}
	ctx, cancel := metaCtx()
	defer cancel()
	return rd.Routines(ctx, p.db, schema)
}

// RoutineSource returns the CREATE statement of a routine.
func (m *Manager) RoutineSource(id, schema string, r Routine) (string, error) {
	p, err := m.pool(id)
	if err != nil {
		return "", err
	}
	rd, ok := p.d.(routineDialect)
	if !ok {
		return "", i18n.Error("이 DB는 프로시저/함수를 지원하지 않습니다")
	}
	ctx, cancel := metaCtx()
	defer cancel()
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	src, err := rd.RoutineSource(ctx, conn, schema, r)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(src) == "" {
		return "", i18n.Errorf("%s의 소스를 볼 수 없습니다 (권한이 없을 수 있습니다)", r.Name)
	}
	return strings.TrimSpace(src) + "\n", nil
}
