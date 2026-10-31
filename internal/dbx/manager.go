package dbx

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type pool struct {
	db  *sql.DB
	d   Dialect
	cfg ConnConfig
	tun *tunnel // nil without SSH
}

func (p *pool) close() {
	p.db.Close()
	if p.tun != nil {
		p.tun.Close()
	}
}

// session is one console tab: it pins a single connection so session state
// (current schema, transactions, temp tables) survives between runs.
type session struct {
	mu     sync.Mutex // held while a statement runs or rows are fetched
	connID string
	conn   *sql.Conn
	schema string
	cur    *cursor

	cancelMu sync.Mutex
	cancel   context.CancelFunc
}

type cursor struct {
	id     string
	rows   *sql.Rows
	cols   []*sql.ColumnType
	cancel context.CancelFunc
}

type Manager struct {
	mu       sync.Mutex
	pools    map[string]*pool
	sessions map[string]*session
}

func NewManager() *Manager {
	return &Manager{pools: map[string]*pool{}, sessions: map[string]*session{}}
}

func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func open(cfg ConnConfig) (*pool, error) {
	d, err := DialectFor(cfg.Driver)
	if err != nil {
		return nil, err
	}
	var tun *tunnel
	dsnCfg := cfg
	if cfg.SSH && cfg.Driver != "sqlite" {
		if tun, err = openTunnel(cfg); err != nil {
			return nil, err
		}
		dsnCfg.Host = "127.0.0.1"
		dsnCfg.Port = tun.localPort()
	}
	db, err := sql.Open(d.DriverName(), d.DSN(dsnCfg))
	if err != nil {
		if tun != nil {
			tun.Close()
		}
		return nil, err
	}
	db.SetMaxIdleConns(4)
	db.SetConnMaxIdleTime(10 * time.Minute)
	if cfg.Driver == "sqlite" {
		db.SetMaxOpenConns(4)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	p := &pool{db: db, d: d, cfg: cfg, tun: tun}
	if err := db.PingContext(ctx); err != nil {
		p.close()
		return nil, err
	}
	return p, nil
}

// Test opens a throwaway connection and returns the server version and,
// with SSH, the server's host key fingerprint.
func Test(cfg ConnConfig) (version, hostKey string, err error) {
	p, err := open(cfg)
	if err != nil {
		return "", "", err
	}
	defer p.close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if p.tun != nil {
		hostKey = p.tun.HostKey()
	}
	return p.d.Version(ctx, p.db), hostKey, nil
}

// SSHHostKey returns the host key fingerprint seen by a connection's tunnel.
func (m *Manager) SSHHostKey(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p := m.pools[id]; p != nil && p.tun != nil {
		return p.tun.HostKey()
	}
	return ""
}

func (m *Manager) Connect(cfg ConnConfig) (string, error) {
	m.Disconnect(cfg.ID)
	p, err := open(cfg)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	m.pools[cfg.ID] = p
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return p.d.Version(ctx, p.db), nil
}

func (m *Manager) Disconnect(id string) {
	m.mu.Lock()
	p := m.pools[id]
	delete(m.pools, id)
	var ss []*session
	for k, s := range m.sessions {
		if s.connID == id {
			ss = append(ss, s)
			delete(m.sessions, k)
		}
	}
	m.mu.Unlock()
	for _, s := range ss {
		s.close()
	}
	if p != nil {
		p.close()
	}
}

func (m *Manager) Connected(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pools[id] != nil
}

func (m *Manager) pool(id string) (*pool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pools[id]
	if p == nil {
		return nil, errors.New("연결되어 있지 않습니다")
	}
	return p, nil
}

func metaCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}

func (m *Manager) Schemas(id string) (SchemaList, error) {
	p, err := m.pool(id)
	if err != nil {
		return SchemaList{}, err
	}
	ctx, cancel := metaCtx()
	defer cancel()
	return p.d.ListSchemas(ctx, p.db)
}

func (m *Manager) Tables(id, schema string) ([]Table, error) {
	p, err := m.pool(id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := metaCtx()
	defer cancel()
	tables, err := p.d.ListTables(ctx, p.db, schema)
	if err != nil {
		return nil, err
	}
	sortTables(tables)
	return tables, nil
}

// sortTables orders by name the way most clients do, case-insensitively by
// character code, so "_sys" comes before "a" regardless of the server's
// collation (which may ignore punctuation). SQL Server's dbo comes first.
func sortTables(ts []Table) {
	sort.SliceStable(ts, func(i, j int) bool {
		a, b := ts[i], ts[j]
		if a.Schema != b.Schema {
			if (a.Schema == "dbo") != (b.Schema == "dbo") {
				return a.Schema == "dbo"
			}
			return strings.ToLower(a.Schema) < strings.ToLower(b.Schema)
		}
		la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if la != lb {
			return la < lb
		}
		return a.Name < b.Name
	})
}

func (m *Manager) Columns(id, schema string, t Table) ([]Column, error) {
	p, err := m.pool(id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := metaCtx()
	defer cancel()
	return p.d.ListColumns(ctx, p.db, schema, t)
}

func (m *Manager) AllColumns(id, schema string) (map[string][]string, error) {
	p, err := m.pool(id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := metaCtx()
	defer cancel()
	return p.d.AllColumns(ctx, p.db, schema)
}

func (m *Manager) SelectSQL(id, schema string, t Table, where, orderBy string, limit int) (string, error) {
	p, err := m.pool(id)
	if err != nil {
		return "", err
	}
	return p.d.Select(p.d.QualifiedName(schema, t), where, orderBy, limit), nil
}

// QuoteIdent quotes a column name for the connection's dialect.
func (m *Manager) QuoteIdent(id, name string) (string, error) {
	p, err := m.pool(id)
	if err != nil {
		return "", err
	}
	return p.quote(name), nil
}

func (m *Manager) QualifiedName(id, schema string, t Table) (string, error) {
	p, err := m.pool(id)
	if err != nil {
		return "", err
	}
	return p.d.QualifiedName(schema, t), nil
}

// ---------------------------------------------------------------- sessions

func (m *Manager) session(tabID, connID string) (*session, *pool, error) {
	p, err := m.pool(connID)
	if err != nil {
		return nil, nil, err
	}
	m.mu.Lock()
	s := m.sessions[tabID]
	if s != nil && s.connID != connID {
		delete(m.sessions, tabID)
		m.mu.Unlock()
		s.close()
		m.mu.Lock()
		s = nil
	}
	if s == nil {
		s = &session{connID: connID}
		m.sessions[tabID] = s
	}
	m.mu.Unlock()
	return s, p, nil
}

func (s *session) close() {
	s.Cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeCursor()
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
}

func (s *session) Cancel() {
	s.cancelMu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.cancelMu.Unlock()
}

// closeCursor drops an open result. Closing unread rows makes most drivers
// drain the rest from the server, so if that takes too long the query is
// cancelled and the connection is replaced.
func (s *session) closeCursor() {
	c := s.cur
	if c == nil {
		return
	}
	s.cur = nil
	done := make(chan struct{})
	go func() {
		c.rows.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		c.cancel()
		<-done
	}
	c.cancel()
}

func (s *session) ensureConn(ctx context.Context, p *pool) error {
	if s.conn != nil {
		if err := s.conn.PingContext(ctx); err == nil {
			return nil
		}
		s.conn.Close()
		s.conn = nil
	}
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return err
	}
	s.conn = conn
	if s.schema != "" {
		if q := p.d.UseSchema(s.schema); q != "" {
			_, _ = conn.ExecContext(ctx, q)
		}
	}
	return nil
}

func (m *Manager) Cancel(tabID string) {
	m.mu.Lock()
	s := m.sessions[tabID]
	m.mu.Unlock()
	if s != nil {
		s.Cancel()
	}
}

func (m *Manager) CloseSession(tabID string) {
	m.mu.Lock()
	s := m.sessions[tabID]
	delete(m.sessions, tabID)
	m.mu.Unlock()
	if s != nil {
		go s.close()
	}
}

var queryKeywords = map[string]bool{
	"SELECT": true, "WITH": true, "SHOW": true, "DESC": true, "DESCRIBE": true, "EXPLAIN": true,
	"PRAGMA": true, "VALUES": true, "TABLE": true, "CALL": true, "EXEC": true, "EXECUTE": true,
	"HELP": true, "CHECK": true, "CHECKSUM": true, "OPTIMIZE": true, "REPAIR": true, "ANALYZE": true,
}

func firstKeyword(stmt string) string {
	sp := splitter{text: stmt}
	return sp.nextWord(0)
}

func returnsRows(stmt, style string) bool {
	kw := firstKeyword(stmt)
	if queryKeywords[kw] {
		return true
	}
	if kw == "" && strings.HasPrefix(strings.TrimSpace(stmt), "(") {
		return true // (SELECT ...) UNION ...
	}
	up := strings.ToUpper(stmt)
	if (kw == "INSERT" || kw == "UPDATE" || kw == "DELETE" || kw == "MERGE") &&
		(strings.Contains(up, "RETURNING") || (style == "mssql" && strings.Contains(up, "OUTPUT"))) {
		return true
	}
	return false
}

// Execute runs the statement under the caret, or the whole script when
// Cursor < 0. Execution stops at the first error.
func (m *Manager) Execute(req ExecRequest) (ExecResponse, error) {
	s, p, err := m.session(req.TabID, req.ConnID)
	if err != nil {
		return ExecResponse{}, err
	}
	fetch := req.Fetch
	if fetch <= 0 {
		fetch = 500
	}

	stmts := Split(req.SQL, p.d.Style())
	if req.Cursor >= 0 {
		st, ok := PickAt(req.SQL, stmts, byteOffset(req.SQL, req.Cursor))
		if !ok {
			return ExecResponse{}, errors.New("실행할 SQL이 없습니다")
		}
		stmts = []Stmt{st}
	}
	if len(stmts) == 0 {
		return ExecResponse{}, errors.New("실행할 SQL이 없습니다")
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancelMu.Lock()
	if s.cancel != nil {
		s.cancel() // a stale cancel from a previous run; harmless
	}
	s.cancel = cancel
	s.cancelMu.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeCursor()

	resp := ExecResponse{}
	if err := s.ensureConn(ctx, p); err != nil {
		cancel()
		return resp, err
	}
	if req.Schema != "" && req.Schema != s.schema {
		if q := p.d.UseSchema(req.Schema); q != "" {
			if _, err := s.conn.ExecContext(ctx, q); err != nil {
				cancel()
				return resp, fmt.Errorf("스키마 변경 실패: %w", err)
			}
		}
		s.schema = req.Schema
	}

	keepCursor := false
	for i, st := range stmts {
		last := i == len(stmts)-1
		r := StmtResult{
			SQL:          st.SQL,
			From:         utf16Len(req.SQL[:st.Start]),
			To:           utf16Len(req.SQL[:st.End]),
			RowsAffected: -1,
		}
		start := time.Now()
		out, abandoned := s.runStmt(ctx, st.SQL, returnsRows(st.SQL, p.d.Style()), fetch)
		r.ElapsedMs = time.Since(start).Milliseconds()
		if abandoned {
			r.Error = "사용자에 의해 취소됨 (서버가 취소에 응답하지 않아 이 탭의 연결을 새로 엽니다)"
			resp.Results = append(resp.Results, r)
			break
		}
		if out.err != nil {
			r.Error = out.err.Error()
		}
		if out.rows != nil {
			r.Source = DetectSource(st.SQL, p.d.Style())
			r.Columns = describeColumns(out.cols)
			r.Rows, r.HasMore = out.data, out.more
			if r.HasMore && last && out.err == nil {
				// Keep the cursor open so "load more" can continue it.
				r.CursorID = NewID()
				s.cur = &cursor{id: r.CursorID, rows: out.rows, cols: out.cols, cancel: cancel}
				keepCursor = true
			} else {
				s.cur = &cursor{rows: out.rows, cancel: func() {}}
				s.closeCursor()
			}
		} else if out.res != nil {
			if n, err := out.res.RowsAffected(); err == nil {
				r.RowsAffected = n
			}
			if pe, ok := p.d.(interface {
				AfterExec(context.Context, Querier, string) string
			}); ok && s.conn != nil {
				r.Error = pe.AfterExec(ctx, s.conn, st.SQL)
			}
		}
		if r.Error != "" && ctx.Err() != nil {
			r.Error = "사용자에 의해 취소됨"
		}
		resp.Results = append(resp.Results, r)
		if r.Error != "" {
			break
		}
	}

	if !keepCursor {
		s.cancelMu.Lock()
		s.cancel = nil
		s.cancelMu.Unlock()
		cancel()
	}
	if s.conn != nil {
		cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
		if !keepCursor {
			if cur := p.d.CurrentSchema(cctx, s.conn); cur != "" {
				s.schema = cur
			}
		}
		ccancel()
	}
	resp.CurrentSchema = s.schema
	return resp, nil
}

type stmtOut struct {
	rows *sql.Rows
	cols []*sql.ColumnType
	data [][]any
	more bool
	res  sql.Result
	err  error
}

// runStmt runs one statement on the session connection. If the context is
// cancelled and the driver still hasn't returned a few seconds later (some
// servers ignore the cancel request), the connection is handed to a cleanup
// goroutine and the session will open a fresh one next time.
func (s *session) runStmt(ctx context.Context, q string, query bool, fetch int) (stmtOut, bool) {
	conn := s.conn
	ch := make(chan stmtOut, 1)
	go func() {
		var o stmtOut
		if query {
			o.rows, o.err = conn.QueryContext(ctx, q)
			if o.err == nil {
				o.cols, _ = o.rows.ColumnTypes()
				o.data, o.more, o.err = readRows(o.rows, o.cols, fetch)
			}
		} else {
			o.res, o.err = conn.ExecContext(ctx, q)
		}
		ch <- o
	}()
	select {
	case o := <-ch:
		return o, false
	case <-ctx.Done():
	}
	select {
	case o := <-ch:
		return o, false
	case <-time.After(3 * time.Second):
	}
	s.conn = nil
	go func() {
		o := <-ch
		if o.rows != nil {
			o.rows.Close()
		}
		conn.Raw(func(any) error { return driver.ErrBadConn }) // discard, don't return to the pool
		conn.Close()
	}()
	return stmtOut{}, true
}

func (m *Manager) FetchMore(tabID, cursorID string, n int) (FetchResponse, error) {
	m.mu.Lock()
	s := m.sessions[tabID]
	m.mu.Unlock()
	if s == nil {
		return FetchResponse{}, errors.New("세션이 없습니다")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur == nil || s.cur.id != cursorID {
		return FetchResponse{}, errors.New("결과 커서가 닫혔습니다. 쿼리를 다시 실행하세요")
	}
	if n <= 0 {
		n = 500
	}
	rows, more, err := readRows(s.cur.rows, s.cur.cols, n)
	if err != nil || !more {
		s.closeCursor()
	}
	return FetchResponse{Rows: rows, HasMore: more && err == nil}, err
}

// ---------------------------------------------------------------- values

func describeColumns(cols []*sql.ColumnType) []ResultColumn {
	out := make([]ResultColumn, len(cols))
	for i, c := range cols {
		out[i] = ResultColumn{Name: c.Name(), Type: strings.ToUpper(c.DatabaseTypeName())}
	}
	return out
}

func readRows(rows *sql.Rows, cols []*sql.ColumnType, limit int) ([][]any, bool, error) {
	out := make([][]any, 0, min(limit, 1000))
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for len(out) < limit {
		if !rows.Next() {
			return out, false, rows.Err()
		}
		if err := rows.Scan(ptrs...); err != nil {
			return out, false, err
		}
		row := make([]any, len(cols))
		for i, v := range vals {
			row[i] = cell(v, cols[i].DatabaseTypeName())
		}
		out = append(out, row)
	}
	return out, true, nil
}

const maxCell = 1 << 20

// cell converts a driver value into something the UI shows verbatim:
// a string, or nil for NULL. Numbers are strings so int64/decimal keep
// their precision in JavaScript.
func cell(v any, dbType string) any {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return plainFloat(clip(x), dbType)
	case []byte:
		if strings.EqualFold(dbType, "UNIQUEIDENTIFIER") && len(x) == 16 {
			return mssqlGUID(x)
		}
		if utf8.Valid(x) && !isBinaryType(dbType) {
			return plainFloat(clip(string(x)), dbType)
		}
		if len(x) > maxCell/2 {
			return fmt.Sprintf("0x%s… (%d bytes)", strings.ToUpper(hex.EncodeToString(x[:256])), len(x))
		}
		return "0x" + strings.ToUpper(hex.EncodeToString(x))
	case time.Time:
		return formatTime(x, dbType)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return formatFloat(x, 64)
	case float32:
		return formatFloat(float64(x), 32)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case *big.Int:
		return x.String()
	case fmt.Stringer:
		return clip(x.String())
	}
	return clip(fmt.Sprint(v))
}

// formatFloat writes floats without an exponent (2.611e+25 ->
// 26110000000000000000000000) unless they are extreme, keeping the
// shortest digits that round-trip.
func formatFloat(f float64, bits int) string {
	if a := math.Abs(f); a != 0 && (a >= 1e64 || a < 1e-30) || math.IsInf(f, 0) || math.IsNaN(f) {
		return strconv.FormatFloat(f, 'g', -1, bits)
	}
	return strconv.FormatFloat(f, 'f', -1, bits)
}

// plainFloat rewrites exponent notation that some servers send as text for
// FLOAT/DOUBLE columns (MySQL/MariaDB: "2.611e25").
func plainFloat(s, dbType string) string {
	if !strings.ContainsAny(s, "eE") || !isFloatType(dbType) {
		return s
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	return formatFloat(f, 64)
}

func isFloatType(t string) bool {
	base, _, _ := strings.Cut(strings.ToUpper(t), "(")
	switch strings.TrimSpace(strings.TrimPrefix(base, "UNSIGNED ")) {
	case "FLOAT", "DOUBLE", "REAL", "DOUBLE PRECISION", "FLOAT4", "FLOAT8",
		"BINARY_FLOAT", "BINARY_DOUBLE", "NUMBER", "DECIMAL", "NUMERIC":
		return true
	}
	return strings.HasPrefix(base, "FLOAT") || strings.HasPrefix(base, "DOUBLE")
}

func clip(s string) string {
	if len(s) > maxCell {
		return s[:maxCell] + "…"
	}
	return s
}

func isBinaryType(t string) bool {
	t = strings.ToUpper(t)
	return strings.Contains(t, "BLOB") || strings.Contains(t, "BINARY") || t == "BYTEA" || t == "RAW" || t == "LONG RAW" || t == "IMAGE" || t == "GEOMETRY"
}

func formatTime(t time.Time, dbType string) string {
	u := strings.ToUpper(dbType)
	if u == "DATE" && t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
		return t.Format("2006-01-02")
	}
	if u == "TIME" {
		return t.Format("15:04:05.999999999")
	}
	s := t.Format("2006-01-02 15:04:05.999999999")
	if strings.Contains(u, "TZ") || strings.Contains(u, "OFFSET") || strings.Contains(u, "TIME ZONE") {
		s += t.Format(" -07:00")
	}
	return s
}

// SQL Server stores GUIDs with the first three groups little-endian.
func mssqlGUID(b []byte) string {
	o := []byte{b[3], b[2], b[1], b[0], b[5], b[4], b[7], b[6], b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15]}
	h := strings.ToUpper(hex.EncodeToString(o))
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
