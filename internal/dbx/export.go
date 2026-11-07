package dbx

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"tablume/internal/i18n"
)

// Export writes whole tables to a file: one table as CSV or JSON, or any
// number as SQL (CREATE and/or INSERT statements). Rows are streamed from
// the server, so the size of a table doesn't matter.

type ExportRequest struct {
	ConnID string  `json:"connId"`
	Schema string  `json:"schema"`
	Tables []Table `json:"tables"` // none: every table and view in the schema
	Format string  `json:"format"` // csv, json, sql
	DDL    bool    `json:"ddl"`    // sql: CREATE statements
	Data   bool    `json:"data"`   // sql: INSERT statements (csv and json are always data)
}

// ExportName is the file name an export suggests.
func ExportName(req ExportRequest) string {
	base := req.Schema
	if len(req.Tables) == 1 {
		base = req.Tables[0].Name
	}
	if base == "" {
		base = "export"
	}
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, base) + "." + req.Format
}

// Export writes the export to w and returns the number of rows written.
func (m *Manager) Export(ctx context.Context, w io.Writer, req ExportRequest) (int64, error) {
	p, err := m.pool(req.ConnID)
	if err != nil {
		return 0, err
	}
	tables := req.Tables
	if len(tables) == 0 {
		if tables, err = m.Tables(req.ConnID, req.Schema); err != nil {
			return 0, err
		}
	}
	bw := bufio.NewWriterSize(w, 64<<10)
	var n int64
	switch req.Format {
	case "csv", "json":
		if len(tables) != 1 {
			return 0, i18n.Error("CSV와 JSON은 테이블 하나씩 내보낼 수 있습니다")
		}
		if req.Format == "csv" {
			n, err = p.exportCSV(ctx, bw, req.Schema, tables[0])
		} else {
			n, err = p.exportJSON(ctx, bw, req.Schema, tables[0])
		}
	case "sql":
		n, err = m.exportSQL(ctx, bw, p, req, tables)
	default:
		return 0, fmt.Errorf("unknown format %q", req.Format)
	}
	if ferr := bw.Flush(); err == nil {
		err = ferr
	}
	return n, err
}

type valKind int

const (
	vNull valKind = iota
	vNum
	vBool
	vBin
	vTime
	vText
)

var (
	numericType = regexp.MustCompile(`^(UNSIGNED )?(TINYINT|SMALLINT|MEDIUMINT|INT|INTEGER|BIGINT|INT[248]|NUMBER|NUMERIC|DECIMAL|FLOAT[48]?|DOUBLE|REAL|MONEY|SMALLMONEY|BINARY_FLOAT|BINARY_DOUBLE)\b`)
	numberLit   = regexp.MustCompile(`^-?\d+(\.\d+)?([eE][-+]?\d+)?$`)
)

// exportValue is cell() without clipping: the value as text and what it is.
// Binary values are their bytes in hex.
func exportValue(v any, dbType string) (string, valKind) {
	num := func(s string) (string, valKind) {
		if numericType.MatchString(strings.ToUpper(dbType)) && numberLit.MatchString(s) {
			return s, vNum
		}
		return s, vText
	}
	switch x := v.(type) {
	case nil:
		return "", vNull
	case string:
		return num(plainFloat(x, dbType))
	case []byte:
		if strings.EqualFold(dbType, "UNIQUEIDENTIFIER") && len(x) == 16 {
			return mssqlGUID(x), vText
		}
		if strings.EqualFold(dbType, "BIT") && len(x) <= 8 { // MySQL/MariaDB: as cell() shows it
			var n uint64
			for _, b := range x {
				n = n<<8 | uint64(b)
			}
			return strconv.FormatUint(n, 10), vNum
		}
		if utf8.Valid(x) && !isBinaryType(dbType) {
			return num(plainFloat(string(x), dbType))
		}
		return strings.ToUpper(hex.EncodeToString(x)), vBin
	case time.Time:
		return formatTime(x, dbType), vTime
	case int64:
		return strconv.FormatInt(x, 10), vNum
	case float64:
		return num(formatFloat(x, 64))
	case float32:
		return num(formatFloat(float64(x), 32))
	case bool:
		return strconv.FormatBool(x), vBool
	case *big.Int:
		return x.String(), vNum
	case fmt.Stringer:
		return num(x.String())
	}
	return num(fmt.Sprint(v))
}

// eachRow streams a table's rows.
func (p *pool) eachRow(ctx context.Context, schema string, t Table, head func(cols []*sql.ColumnType) error, row func(vals []any) error) (int64, error) {
	rows, err := p.db.QueryContext(ctx, "SELECT * FROM "+p.d.QualifiedName(schema, t))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	cols, err := rows.ColumnTypes()
	if err != nil {
		return 0, err
	}
	if err := head(cols); err != nil {
		return 0, err
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	var n int64
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return n, err
		}
		if err := row(vals); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func (p *pool) exportCSV(ctx context.Context, w *bufio.Writer, schema string, t Table) (int64, error) {
	w.WriteString("\uFEFF") // so Excel reads it as UTF-8
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	var types []string
	rec := []string{}
	n, err := p.eachRow(ctx, schema, t, func(cols []*sql.ColumnType) error {
		for _, c := range cols {
			rec = append(rec, c.Name())
			types = append(types, c.DatabaseTypeName())
		}
		return cw.Write(rec)
	}, func(vals []any) error {
		for i, v := range vals {
			s, k := exportValue(v, types[i])
			if k == vBin {
				s = "0x" + s
			}
			rec[i] = s // NULL is an empty field
		}
		return cw.Write(rec)
	})
	cw.Flush()
	if err == nil {
		err = cw.Error()
	}
	return n, err
}

func (p *pool) exportJSON(ctx context.Context, w *bufio.Writer, schema string, t Table) (int64, error) {
	var keys []string
	var types []string
	first := true
	w.WriteString("[")
	n, err := p.eachRow(ctx, schema, t, func(cols []*sql.ColumnType) error {
		for _, c := range cols {
			k, _ := json.Marshal(c.Name())
			keys = append(keys, string(k))
			types = append(types, c.DatabaseTypeName())
		}
		return nil
	}, func(vals []any) error {
		if !first {
			w.WriteString(",")
		}
		first = false
		w.WriteString("\n  {")
		for i, v := range vals {
			if i > 0 {
				w.WriteString(", ")
			}
			w.WriteString(keys[i] + ": ")
			s, k := exportValue(v, types[i])
			switch k {
			case vNull:
				w.WriteString("null")
			case vNum, vBool:
				w.WriteString(s) // number literals are valid JSON as they are
			default:
				if k == vBin {
					s = "0x" + s
				}
				b, _ := json.Marshal(s)
				w.Write(b)
			}
		}
		w.WriteString("}")
		return nil
	})
	w.WriteString("\n]\n")
	return n, err
}

// sqlLiteral writes a value as the engine reads it back in an INSERT.
func sqlLiteral(s string, k valKind, style string) string {
	switch k {
	case vNull:
		return "NULL"
	case vNum:
		return s
	case vBool:
		if style == "postgres" {
			return strings.ToUpper(s)
		}
		if s == "true" {
			return "1"
		}
		return "0"
	case vBin:
		switch style {
		case "postgres":
			return `'\x` + s + "'"
		case "mssql":
			return "0x" + s
		case "oracle":
			return "HEXTORAW('" + s + "')"
		}
		return "X'" + s + "'"
	}
	q := strings.ReplaceAll(s, "'", "''")
	switch style {
	case "mysql":
		q = strings.ReplaceAll(q, `\`, `\\`)
	case "mssql":
		return "N'" + q + "'"
	case "oracle":
		// a date/time literal doesn't depend on the session's NLS format
		if k == vTime && len(s) > 10 && !hasOffset(s) {
			return "TIMESTAMP '" + q + "'"
		}
		if k == vTime && len(s) == 10 {
			return "DATE '" + q + "'"
		}
	}
	return "'" + q + "'"
}

func (m *Manager) exportSQL(ctx context.Context, w *bufio.Writer, p *pool, req ExportRequest, tables []Table) (int64, error) {
	style := p.d.Style()
	fmt.Fprintf(w, "-- Tablume export: %s (%s)\n-- %s\n\n", req.Schema, p.cfg.Driver, time.Now().Format("2006-01-02 15:04:05"))
	tables = m.dependencyOrder(req.ConnID, req.Schema, tables)
	if style == "mysql" && req.Data {
		w.WriteString("SET FOREIGN_KEY_CHECKS = 0;\n\n")
	}
	if req.DDL {
		for _, t := range tables {
			ddl, err := m.DDL(req.ConnID, req.Schema, t)
			if err != nil {
				return 0, err
			}
			ddl = strings.TrimSpace(ddl)
			if !strings.HasSuffix(ddl, ";") && !strings.HasSuffix(ddl, "/") {
				ddl += ";"
			}
			w.WriteString(ddl + "\n\n")
		}
	}
	var total int64
	if req.Data {
		perStmt := 100 // rows per INSERT
		if style == "oracle" {
			perStmt = 1
		}
		for _, t := range tables {
			if isView(t) {
				continue
			}
			// The table's own name, as the CREATE statements have it: the file
			// runs in whatever schema it is run in.
			target := p.quote(t.Name)
			// SQL Server refuses values for an identity column unless asked
			identity := false
			if style == "mssql" {
				var v sql.NullInt64
				_ = p.db.QueryRowContext(ctx, "SELECT OBJECTPROPERTY(OBJECT_ID(@p1), 'TableHasIdentity')", p.d.QualifiedName(req.Schema, t)).Scan(&v)
				identity = v.Int64 == 1
			}
			if identity {
				w.WriteString("SET IDENTITY_INSERT " + target + " ON;\n")
			}
			var head string
			var types []string
			k := 0
			n, err := p.eachRow(ctx, req.Schema, t, func(cols []*sql.ColumnType) error {
				names := make([]string, len(cols))
				for i, c := range cols {
					names[i] = p.quote(c.Name())
					types = append(types, c.DatabaseTypeName())
				}
				head = "INSERT INTO " + target + " (" + strings.Join(names, ", ") + ") VALUES "
				return nil
			}, func(vals []any) error {
				if k == 0 {
					w.WriteString(head)
				} else {
					w.WriteString(",\n  ")
				}
				w.WriteString("(")
				for i, v := range vals {
					if i > 0 {
						w.WriteString(", ")
					}
					s, kind := exportValue(v, types[i])
					w.WriteString(sqlLiteral(s, kind, style))
				}
				w.WriteString(")")
				if k++; k == perStmt {
					w.WriteString(";\n")
					k = 0
				}
				return ctx.Err()
			})
			if k > 0 {
				w.WriteString(";\n")
			}
			if identity {
				w.WriteString("SET IDENTITY_INSERT " + target + " OFF;\n")
			}
			if n > 0 {
				w.WriteString("\n")
			}
			total += n
			if err != nil {
				return total, fmt.Errorf("%s: %w", t.Name, err)
			}
		}
	}
	if style == "mysql" && req.Data {
		w.WriteString("SET FOREIGN_KEY_CHECKS = 1;\n")
	}
	return total, nil
}

// hasOffset: a time ending in a zone offset (" +09:00").
func hasOffset(s string) bool {
	return len(s) > 6 && (s[len(s)-6] == '+' || s[len(s)-6] == '-') && s[len(s)-3] == ':'
}

func isView(t Table) bool { return strings.Contains(strings.ToUpper(t.Type), "VIEW") }

// dependencyOrder puts referenced tables before the tables that refer to
// them (so the file runs in order), then views. Tables in a cycle keep
// their place at the end.
func (m *Manager) dependencyOrder(id, schema string, tables []Table) []Table {
	var base, views []Table
	for _, t := range tables {
		if isView(t) {
			views = append(views, t)
		} else {
			base = append(base, t)
		}
	}
	deps := map[string][]string{}
	for _, t := range base {
		fks, _ := m.ListForeignKeys(id, schema, t) // ordering is a nicety: on error, keep the order
		for _, fk := range fks {
			if fk.RefSchema == "" && fk.RefTable.Name != t.Name {
				deps[t.Name] = append(deps[t.Name], fk.RefTable.Name)
			}
		}
	}
	var out []Table
	done := map[string]bool{}
	for len(out) < len(base) {
		moved := false
		for _, t := range base {
			if done[t.Name] {
				continue
			}
			ready := true
			for _, d := range deps[t.Name] {
				if !done[d] && slices.ContainsFunc(base, func(x Table) bool { return x.Name == d }) {
					ready = false
				}
			}
			if ready {
				out = append(out, t)
				done[t.Name] = true
				moved = true
			}
		}
		if !moved { // a cycle
			for _, t := range base {
				if !done[t.Name] {
					out = append(out, t)
					done[t.Name] = true
				}
			}
		}
	}
	return append(out, views...)
}
