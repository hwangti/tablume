package dbx

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------- source detection

type sqlToken struct {
	kind  byte // 'w' word, 'q' quoted identifier, 's' string literal, 'p' punctuation
	text  string
	depth int // parenthesis depth
	pos   int // byte offset in the source
}

// lexSQL is a small tokenizer, good enough to find the FROM clause.
func lexSQL(s, style string) []sqlToken {
	var out []sqlToken
	depth := 0
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '-' && i+1 < len(s) && s[i+1] == '-', c == '#' && style == "mysql":
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			e := strings.Index(s[i+2:], "*/")
			if e < 0 {
				return out
			}
			i += e + 4
		case c == '\'':
			sp := splitter{text: s}
			j := sp.skipQuoted(i+1, '\'', style == "mysql")
			out = append(out, sqlToken{'s', s[i:j], depth, i})
			i = j
		case c == '"' || c == '`' || (c == '[' && style == "mssql"):
			closer := c
			if c == '[' {
				closer = ']'
			}
			sp := splitter{text: s}
			j := sp.skipQuoted(i+1, closer, false)
			out = append(out, sqlToken{'q', strings.ReplaceAll(s[i+1:max(i+1, j-1)], string(closer)+string(closer), string(closer)), depth, i})
			i = j
		case isIdentStart(c):
			j := i
			for j < len(s) && isIdentChar(s[j]) {
				j++
			}
			out = append(out, sqlToken{'w', s[i:j], depth, i})
			i = j
		default:
			if c == '(' {
				depth++
			}
			out = append(out, sqlToken{'p', string(c), depth, i})
			if c == ')' {
				depth--
			}
			i++
		}
	}
	return out
}

var notSingleTable = map[string]bool{
	"JOIN": true, "UNION": true, "INTERSECT": true, "EXCEPT": true, "MINUS": true,
	"GROUP": true, "HAVING": true, "DISTINCT": true, "CONNECT": true, "PIVOT": true, "UNPIVOT": true,
	"LATERAL": true, "APPLY": true, "INTO": true,
}

var fromEnders = map[string]bool{
	"WHERE": true, "ORDER": true, "LIMIT": true, "FETCH": true, "OFFSET": true, "FOR": true, "WINDOW": true,
}

// DetectSource returns the identifier parts of the single table a SELECT
// reads from, or nil when the query isn't a plain single-table SELECT.
func DetectSource(stmt, style string) []string {
	toks := lexSQL(stmt, style)
	if len(toks) == 0 || toks[0].kind != 'w' || !strings.EqualFold(toks[0].text, "SELECT") {
		return nil
	}
	from := -1
	for i, t := range toks {
		if t.depth != 0 || t.kind != 'w' {
			continue
		}
		u := strings.ToUpper(t.text)
		if notSingleTable[u] {
			return nil
		}
		if u == "FROM" && from < 0 {
			from = i
		}
	}
	if from < 0 {
		return nil
	}
	var parts []string
	i := from + 1
	for {
		if i >= len(toks) || (toks[i].kind != 'w' && toks[i].kind != 'q') || toks[i].depth != 0 {
			return nil
		}
		parts = append(parts, normIdent(toks[i], style))
		i++
		if i < len(toks) && toks[i].kind == 'p' && toks[i].text == "." {
			i++
			continue
		}
		break
	}
	// optional alias
	if i < len(toks) && toks[i].kind == 'w' && strings.EqualFold(toks[i].text, "AS") {
		i++
	}
	if i < len(toks) && (toks[i].kind == 'q' || (toks[i].kind == 'w' && !fromEnders[strings.ToUpper(toks[i].text)])) {
		i++
	}
	if i < len(toks) {
		t := toks[i]
		if t.kind == 'p' && t.text == ";" {
			return parts
		}
		if t.kind != 'w' || !fromEnders[strings.ToUpper(t.text)] {
			return nil // comma join, table sample, etc.
		}
	}
	if len(parts) > 3 {
		return nil
	}
	return parts
}

func normIdent(t sqlToken, style string) string {
	if t.kind == 'q' {
		return t.text
	}
	switch style {
	case "oracle":
		return strings.ToUpper(t.text)
	case "postgres":
		return strings.ToLower(t.text)
	}
	return t.text
}

// ---------------------------------------------------------------- edit info

type EditInfo struct {
	Editable bool `json:"editable"`
	// Insertable: new rows can be added even when existing rows can't be
	// changed (no primary key) — inserts don't need to identify a row.
	Insertable bool   `json:"insertable"`
	Reason     string `json:"reason"`
	Schema     string `json:"schema"`
	Table      Table  `json:"table"`
	// Columns maps each result column to its table column ("" = read-only).
	Columns []string `json:"columns"`
	Key     []string `json:"key"`
	// Keys describes each result column for header icons: "PK", "UK",
	// "FK" (comma separated) or ""; Refs holds the FK target.
	Keys []string `json:"keys"`
	Refs []string `json:"refs"`
	// Comments holds each result column's comment (header tooltip).
	Comments []string `json:"comments"`
}

func (m *Manager) resolveSource(p *pool, current string, parts []string) (string, Table) {
	n := len(parts)
	if p.cfg.Driver == "mssql" {
		switch n {
		case 1:
			return current, Table{Schema: "dbo", Name: parts[0]}
		case 2:
			return current, Table{Schema: parts[0], Name: parts[1]}
		default:
			return parts[0], Table{Schema: parts[1], Name: parts[2]}
		}
	}
	switch n {
	case 1:
		return current, Table{Name: parts[0]}
	case 2:
		return parts[0], Table{Name: parts[1]}
	default:
		return parts[1], Table{Name: parts[2]}
	}
}

// editableType: binary columns and Oracle LONG can't be edited as text.
// (Only the exact LONG type — "LONGTEXT" is ordinary MySQL text.)
func editableType(t string) bool {
	base, _, _ := strings.Cut(t, "(") // RAW(10) -> RAW
	base = strings.ToUpper(strings.TrimSpace(base))
	return !isBinaryType(base) && base != "LONG"
}

// EditInfo tells whether a result set can be edited in place: it must come
// from a single table whose primary key columns are all in the result.
func (m *Manager) EditInfo(id, currentSchema string, source []string, resultCols []string) (EditInfo, error) {
	p, err := m.pool(id)
	if err != nil {
		return EditInfo{}, err
	}
	if len(source) == 0 {
		return EditInfo{Reason: "단일 테이블 SELECT 결과만 편집할 수 있습니다 (JOIN, GROUP BY, UNION 등은 불가)"}, nil
	}
	schema, t := m.resolveSource(p, currentSchema, source)
	ctx, cancel := metaCtx()
	defer cancel()
	cols, err := p.d.ListColumns(ctx, p.db, schema, t)
	if err != nil {
		return EditInfo{}, err
	}
	if len(cols) == 0 {
		return EditInfo{Reason: fmt.Sprintf("테이블 %s을(를) 찾을 수 없습니다", strings.Join(source, "."))}, nil
	}
	info := EditInfo{Schema: schema, Table: t, Columns: make([]string, len(resultCols)),
		Keys: make([]string, len(resultCols)), Refs: make([]string, len(resultCols)),
		Comments: make([]string, len(resultCols))}
	byName := map[string]Column{}
	for _, c := range cols {
		byName[strings.ToUpper(c.Name)] = c
		if c.PK {
			info.Key = append(info.Key, c.Name)
		}
	}
	info.Insertable = true
	seen := map[string]bool{}
	for i, rc := range resultCols {
		if c, ok := byName[strings.ToUpper(rc)]; ok && !seen[c.Name] {
			seen[c.Name] = true
			var kinds []string
			if c.PK {
				kinds = append(kinds, "PK")
			}
			if c.Unique {
				kinds = append(kinds, "UK")
			}
			if c.FK != "" {
				kinds = append(kinds, "FK")
				info.Refs[i] = c.FK
			}
			info.Keys[i] = strings.Join(kinds, ",")
			info.Comments[i] = c.Comment
			if editableType(c.Type) {
				info.Columns[i] = c.Name
			}
		}
	}
	if len(info.Key) == 0 {
		info.Reason = "기본 키(PK)가 없어 기존 행은 수정·삭제할 수 없습니다 (행 추가는 가능)"
		return info, nil
	}
	for _, k := range info.Key {
		if !seen[k] {
			info.Reason = fmt.Sprintf("결과에 기본 키 컬럼 %s이(가) 없어 기존 행은 수정·삭제할 수 없습니다 (행 추가는 가능)", k)
			return info, nil
		}
	}
	info.Editable = true
	return info, nil
}

// ---------------------------------------------------------------- apply

// Values are the grid's text (string) or nil for NULL. (map[string]*string
// would be clearer, but Wails' TypeScript generator can't handle it.)
type RowChange struct {
	Kind   string         `json:"kind"` // update, delete, insert
	Key    map[string]any `json:"key"`
	Values map[string]any `json:"values"`
}

func strArg(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return x
	case *string:
		if x == nil {
			return nil
		}
		return *x
	}
	return fmt.Sprint(v)
}

type ApplyRequest struct {
	ConnID  string      `json:"connId"`
	Schema  string      `json:"schema"`
	Table   Table       `json:"table"`
	Changes []RowChange `json:"changes"`
}

type stmtArgs struct {
	sql  string
	args []any
	one  bool // must affect exactly one row
}

func (p *pool) quote(ident string) string {
	switch p.cfg.Driver {
	case "mysql", "mariadb":
		return quoteWith(ident, "`", "`")
	case "mssql":
		return msq(ident)
	}
	return quoteWith(ident, `"`, `"`)
}

func (p *pool) buildChange(target string, types map[string]string, c RowChange) (stmtArgs, error) {
	var args []any
	param := func(col string, v any) string {
		args = append(args, strArg(v))
		return p.d.Param(len(args), types[col])
	}
	where := func() (string, error) {
		if len(c.Key) == 0 {
			return "", errors.New("행을 식별할 키 값이 없습니다")
		}
		var conds []string
		for _, col := range sortedKeys(c.Key) {
			if _, ok := types[col]; !ok {
				return "", fmt.Errorf("알 수 없는 컬럼: %s", col)
			}
			v := strArg(c.Key[col])
			if v == nil {
				conds = append(conds, p.quote(col)+" IS NULL")
			} else {
				conds = append(conds, p.quote(col)+" = "+param(col, v))
			}
		}
		return strings.Join(conds, " AND "), nil
	}
	switch c.Kind {
	case "update":
		if len(c.Values) == 0 {
			return stmtArgs{}, errors.New("변경할 값이 없습니다")
		}
		var sets []string
		for _, col := range sortedKeys(c.Values) {
			if _, ok := types[col]; !ok {
				return stmtArgs{}, fmt.Errorf("알 수 없는 컬럼: %s", col)
			}
			sets = append(sets, p.quote(col)+" = "+param(col, c.Values[col]))
		}
		w, err := where()
		if err != nil {
			return stmtArgs{}, err
		}
		return stmtArgs{"UPDATE " + target + " SET " + strings.Join(sets, ", ") + " WHERE " + w, args, true}, nil
	case "delete":
		w, err := where()
		if err != nil {
			return stmtArgs{}, err
		}
		return stmtArgs{"DELETE FROM " + target + " WHERE " + w, args, true}, nil
	case "insert":
		var cols, vals []string
		for _, col := range sortedKeys(c.Values) {
			if _, ok := types[col]; !ok {
				return stmtArgs{}, fmt.Errorf("알 수 없는 컬럼: %s", col)
			}
			cols = append(cols, p.quote(col))
			vals = append(vals, param(col, c.Values[col]))
		}
		if len(cols) == 0 {
			if p.cfg.Driver == "mysql" || p.cfg.Driver == "mariadb" {
				return stmtArgs{"INSERT INTO " + target + " () VALUES ()", nil, false}, nil
			}
			return stmtArgs{"INSERT INTO " + target + " DEFAULT VALUES", nil, false}, nil
		}
		return stmtArgs{"INSERT INTO " + target + " (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(vals, ", ") + ")", args, false}, nil
	}
	return stmtArgs{}, fmt.Errorf("알 수 없는 변경 종류: %s", c.Kind)
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// PreviewChanges returns the SQL that ApplyChanges would run (with values
// shown as parameters), for the confirmation view.
func (m *Manager) PreviewChanges(req ApplyRequest) ([]string, error) {
	p, target, types, err := m.applyTarget(req)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range req.Changes {
		st, err := p.buildChange(target, types, c)
		if err != nil {
			return nil, err
		}
		out = append(out, inlineArgs(st.sql, st.args, p))
	}
	return out, nil
}

func (m *Manager) applyTarget(req ApplyRequest) (*pool, string, map[string]string, error) {
	p, err := m.pool(req.ConnID)
	if err != nil {
		return nil, "", nil, err
	}
	ctx, cancel := metaCtx()
	defer cancel()
	cols, err := p.d.ListColumns(ctx, p.db, req.Schema, req.Table)
	if err != nil {
		return nil, "", nil, err
	}
	types := map[string]string{}
	for _, c := range cols {
		types[c.Name] = c.Type
	}
	return p, p.d.QualifiedName(req.Schema, req.Table), types, nil
}

// ApplyChanges runs all changes in one transaction. Updates and deletes must
// each hit exactly one row, otherwise everything is rolled back.
func (m *Manager) ApplyChanges(req ApplyRequest) (int, error) {
	p, target, types, err := m.applyTarget(req)
	if err != nil {
		return 0, err
	}
	var stmts []stmtArgs
	for _, c := range req.Changes {
		st, err := p.buildChange(target, types, c)
		if err != nil {
			return 0, err
		}
		stmts = append(stmts, st)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for i, st := range stmts {
		res, err := tx.ExecContext(ctx, st.sql, st.args...)
		if err != nil {
			return 0, fmt.Errorf("%d번째 변경 실패 (모두 취소됨): %w\n%s", i+1, err, inlineArgs(st.sql, st.args, p))
		}
		if st.one {
			if n, err := res.RowsAffected(); err == nil && n != 1 {
				return 0, fmt.Errorf("%d번째 변경이 %d개 행에 적용되어 모두 취소했습니다. 다른 곳에서 바뀌었을 수 있으니 새로고침하세요.\n%s",
					i+1, n, inlineArgs(st.sql, st.args, p))
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(stmts), nil
}

// inlineArgs renders a statement with literal values, for display only.
func inlineArgs(q string, args []any, p *pool) string {
	lit := func(a any) string {
		if a == nil {
			return "NULL"
		}
		return "'" + strings.ReplaceAll(fmt.Sprint(a), "'", "''") + "'"
	}
	for i := len(args); i >= 1; i-- {
		var ph string
		switch p.cfg.Driver {
		case "postgres":
			ph = fmt.Sprintf("$%d", i)
		case "mssql":
			ph = fmt.Sprintf("@p%d", i)
		case "oracle":
			ph = fmt.Sprintf(":%d", i)
		default:
			continue
		}
		q = replaceParam(q, ph, lit(args[i-1]))
	}
	if p.cfg.Driver == "mysql" || p.cfg.Driver == "mariadb" || p.cfg.Driver == "sqlite" {
		var b strings.Builder
		k := 0
		for _, r := range q {
			if r == '?' && k < len(args) {
				b.WriteString(lit(args[k]))
				k++
			} else {
				b.WriteRune(r)
			}
		}
		q = b.String()
	}
	return q
}

// replaceParam replaces ph only where it isn't followed by another digit
// (so $1 doesn't match inside $10).
func replaceParam(q, ph, val string) string {
	var b strings.Builder
	for {
		i := strings.Index(q, ph)
		if i < 0 {
			b.WriteString(q)
			return b.String()
		}
		end := i + len(ph)
		if end < len(q) && q[end] >= '0' && q[end] <= '9' {
			b.WriteString(q[:end])
			q = q[end:]
			continue
		}
		b.WriteString(q[:i] + val)
		q = q[end:]
	}
}

// ---------------------------------------------------------------- parameters
// Values arrive as the text the grid shows; each dialect converts it.

func (mysqlDialect) Param(int, string) string  { return "?" }
func (sqliteDialect) Param(int, string) string { return "?" }
func (mssqlDialect) Param(n int, _ string) string {
	return fmt.Sprintf("@p%d", n)
}

// PostgreSQL: bind as text and cast, so "123" works for an integer column.
func (pgDialect) Param(n int, typ string) string {
	if typ == "" {
		return fmt.Sprintf("$%d", n)
	}
	return fmt.Sprintf("CAST($%d::text AS %s)", n, typ)
}

// Oracle: dates are shown as "YYYY-MM-DD HH24:MI:SS[.FF]"; parse them back
// explicitly instead of relying on the session's NLS settings.
func (oracleDialect) Param(n int, typ string) string {
	u := strings.ToUpper(typ)
	switch {
	case u == "DATE":
		return fmt.Sprintf("TO_DATE(:%d, 'YYYY-MM-DD HH24:MI:SS')", n)
	case strings.HasPrefix(u, "TIMESTAMP") && strings.Contains(u, "TIME ZONE"):
		return fmt.Sprintf("TO_TIMESTAMP_TZ(:%d, 'YYYY-MM-DD HH24:MI:SS.FF TZH:TZM')", n)
	case strings.HasPrefix(u, "TIMESTAMP"):
		return fmt.Sprintf("TO_TIMESTAMP(:%d, 'YYYY-MM-DD HH24:MI:SS.FF')", n)
	}
	return fmt.Sprintf(":%d", n)
}
