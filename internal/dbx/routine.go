package dbx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
		return "", errors.New("이 DB는 프로시저/함수를 지원하지 않습니다")
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
		return "", fmt.Errorf("%s의 소스를 볼 수 없습니다 (권한이 없을 수 있습니다)", r.Name)
	}
	return strings.TrimSpace(src) + "\n", nil
}

// ---------------------------------------------------------------- MySQL

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

// ---------------------------------------------------------------- PostgreSQL

func (pgDialect) Routines(ctx context.Context, q Querier, schema string) ([]Routine, error) {
	return scanRoutines(ctx, q, `SELECT n.nspname, p.proname,
		CASE p.prokind WHEN 'p' THEN 'PROCEDURE' ELSE 'FUNCTION' END,
		pg_get_function_identity_arguments(p.oid), p.oid::text
		FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = $1 AND p.prokind IN ('f', 'p')
		ORDER BY 3 DESC, 2, 4`, schema)
}

func (pgDialect) RoutineSource(ctx context.Context, conn *sql.Conn, schema string, r Routine) (string, error) {
	var src sql.NullString
	err := conn.QueryRowContext(ctx, "SELECT pg_get_functiondef($1::oid)", r.ID).Scan(&src)
	if err != nil {
		return "", err
	}
	// The body is dollar-quoted, so it already runs as one statement.
	return strings.TrimSpace(src.String) + ";", nil
}

// ---------------------------------------------------------------- SQL Server

func (mssqlDialect) Routines(ctx context.Context, q Querier, db string) ([]Routine, error) {
	d := msq(db)
	return scanRoutines(ctx, q, `SELECT s.name, o.name,
		CASE WHEN o.type = 'P' THEN 'PROCEDURE' ELSE 'FUNCTION' END, '', ''
		FROM `+d+`.sys.objects o JOIN `+d+`.sys.schemas s ON s.schema_id = o.schema_id
		WHERE o.type IN ('P', 'FN', 'IF', 'TF') AND o.is_ms_shipped = 0
		ORDER BY 3 DESC, CASE WHEN s.name = 'dbo' THEN 0 ELSE 1 END, s.name, o.name`)
}

func (mssqlDialect) RoutineSource(ctx context.Context, conn *sql.Conn, db string, r Routine) (string, error) {
	d := msq(db)
	var src sql.NullString
	err := conn.QueryRowContext(ctx, `SELECT m.definition FROM `+d+`.sys.sql_modules m
		JOIN `+d+`.sys.objects o ON o.object_id = m.object_id
		JOIN `+d+`.sys.schemas s ON s.schema_id = o.schema_id
		WHERE s.name = @p1 AND o.name = @p2`, r.Schema, r.Name).Scan(&src)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(src.String) + "\nGO", nil
}

// ---------------------------------------------------------------- Oracle

func (oracleDialect) Routines(ctx context.Context, q Querier, schema string) ([]Routine, error) {
	return scanRoutines(ctx, q, `SELECT owner, object_name, object_type, '', ''
		FROM all_objects WHERE owner = :1 AND object_type IN ('PROCEDURE', 'FUNCTION', 'PACKAGE')
		ORDER BY CASE object_type WHEN 'PACKAGE' THEN 0 WHEN 'PROCEDURE' THEN 1 ELSE 2 END, object_name`, schema)
}

func (oracleDialect) RoutineSource(ctx context.Context, conn *sql.Conn, schema string, r Routine) (string, error) {
	_, _ = conn.ExecContext(ctx, `BEGIN
		DBMS_METADATA.SET_TRANSFORM_PARAM(DBMS_METADATA.SESSION_TRANSFORM, 'SQLTERMINATOR', TRUE);
		DBMS_METADATA.SET_TRANSFORM_PARAM(DBMS_METADATA.SESSION_TRANSFORM, 'PRETTY', TRUE);
	END;`)
	var src sql.NullString
	// PACKAGE returns the specification and the body together.
	if err := conn.QueryRowContext(ctx, "SELECT DBMS_METADATA.GET_DDL(:1, :2, :3) FROM dual", r.Type, r.Name, schema).Scan(&src); err != nil {
		return "", err
	}
	return strings.TrimSpace(src.String), nil
}
