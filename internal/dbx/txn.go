package dbx

import (
	"context"
	"database/sql/driver"
	"time"

	"tablume/internal/i18n"
)

// Manual commit: a tab keeps its changes (statements it runs, grid edits)
// in an open transaction until Commit or Rollback. A transaction is begun
// before the first change; Oracle's driver commits every statement unless a
// transaction is open through it, so there it is begun through the driver.
// Reads before that run on their own and see what others committed.
//
// (Session switches are not used: with MySQL's autocommit=0 even reads hold
// a snapshot, so a refresh doesn't show others' changes; with SQL Server's
// IMPLICIT_TRANSACTIONS, BEGIN TRANSACTION nests and a typed COMMIT would
// leave the changes open.)

// Statements that control the transaction or the session themselves: no
// BEGIN is sent before them.
var txControl = map[string]bool{
	"BEGIN": true, "START": true, "COMMIT": true, "ROLLBACK": true, "END": true, "ABORT": true,
	"SAVEPOINT": true, "RELEASE": true, "SET": true, "USE": true,
}

func (s *session) setManual(ctx context.Context, style string, on bool) error {
	if on == s.manual {
		return nil
	}
	if !on && s.inTx {
		return i18n.Error("커밋하지 않은 변경이 있습니다. 먼저 커밋하거나 롤백하세요")
	}
	s.manual = on
	return nil
}

// beforeStmt opens the transaction before the first change in manual mode.
func (s *session) beforeStmt(ctx context.Context, style, stmt string) error {
	if !s.manual || s.inTx || readsOnly(stmt, style) {
		return nil
	}
	// (Oracle's BEGIN starts a PL/SQL block, not a transaction)
	if kw := firstKeyword(stmt); txControl[kw] && !(kw == "BEGIN" && style == "oracle") {
		return nil
	}
	return s.beginTx(ctx, style)
}

// beginTx opens the tab's transaction unless one is open.
func (s *session) beginTx(ctx context.Context, style string) error {
	if s.inTx {
		return nil
	}
	q := map[string]string{
		"postgres": "BEGIN",
		"sqlite":   "BEGIN",
		"mysql":    "START TRANSACTION",
		"mssql":    "IF @@TRANCOUNT = 0 BEGIN TRANSACTION",
	}[style]
	switch {
	case q != "":
		if _, err := s.conn.ExecContext(ctx, q); err != nil {
			return err
		}
	case style == "oracle":
		err := s.conn.Raw(func(dc any) error {
			b, ok := dc.(driver.ConnBeginTx)
			if !ok {
				return i18n.Error("이 드라이버는 수동 커밋을 지원하지 않습니다")
			}
			tx, err := b.BeginTx(ctx, driver.TxOptions{})
			s.otx = tx
			return err
		})
		if err != nil {
			return err
		}
	}
	s.inTx = true
	return nil
}

var implicitCommit = map[string]bool{
	"CREATE": true, "ALTER": true, "DROP": true, "RENAME": true, "TRUNCATE": true,
	"GRANT": true, "REVOKE": true, "LOCK": true, "UNLOCK": true, "ANALYZE": true, "OPTIMIZE": true,
}

// afterStmt follows a COMMIT, ROLLBACK or BEGIN the user ran themselves.
func (s *session) afterStmt(style, stmt string) {
	if !s.manual {
		return
	}
	ws := sqlWords(stmt, style)
	if len(ws) == 0 {
		return
	}
	// DDL ends the transaction on MySQL and Oracle (an implicit commit); the
	// next change begins another
	if implicitCommit[ws[0].w] && (style == "mysql" || style == "oracle") {
		s.forgetTx(true)
		return
	}
	switch ws[0].w {
	case "BEGIN", "START":
		if style != "oracle" {
			s.inTx = true
		}
	case "COMMIT", "END":
		s.forgetTx(true)
	case "ROLLBACK", "ABORT":
		for _, w := range ws[1:] {
			if w.w == "TO" {
				return // ROLLBACK TO SAVEPOINT: still in the transaction
			}
		}
		s.forgetTx(false)
	}
}

// forgetTx marks the transaction as ended by the server; Oracle's driver
// is told too, so it stops holding statements back.
func (s *session) forgetTx(commit bool) {
	if s.otx != nil {
		if commit {
			_ = s.otx.Commit()
		} else {
			_ = s.otx.Rollback()
		}
		s.otx = nil
	}
	s.inTx = false
}

func (s *session) endTx(ctx context.Context, style string, commit bool) error {
	if !s.inTx || s.conn == nil {
		s.inTx, s.otx = false, nil
		return nil
	}
	if s.otx != nil {
		tx := s.otx
		s.otx, s.inTx = nil, false
		if commit {
			return tx.Commit()
		}
		return tx.Rollback()
	}
	q := "ROLLBACK"
	if commit {
		q = "COMMIT"
	}
	if style == "mssql" {
		q = "IF @@TRANCOUNT > 0 ROLLBACK"
		if commit {
			q = "WHILE @@TRANCOUNT > 0 COMMIT" // also a BEGIN TRANSACTION the user nested
		}
	}
	if _, err := s.conn.ExecContext(ctx, q); err != nil {
		return err
	}
	s.inTx = false
	return nil
}

// EndTx commits or rolls back a tab's open transaction (manual commit).
func (m *Manager) EndTx(tabID string, commit bool) error {
	m.mu.Lock()
	s := m.sessions[tabID]
	m.mu.Unlock()
	if s == nil {
		return nil
	}
	p, err := m.pool(s.connID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeCursor()
	ctx, cancel := metaCtx()
	defer cancel()
	return s.endTx(ctx, p.d.Style(), commit)
}

// TxOpen reports whether a tab has changes waiting for Commit or Rollback.
func (m *Manager) TxOpen(tabID string) bool {
	m.mu.Lock()
	s := m.sessions[tabID]
	m.mu.Unlock()
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inTx
}

// applyInTx runs grid changes in the tab's transaction (manual commit): they
// wait for Commit like the tab's statements. A savepoint keeps the batch
// all-or-nothing without ending the transaction.
func (m *Manager) applyInTx(req ApplyRequest, p *pool, stmts []stmtArgs) error {
	s, _, err := m.session(req.TabID, req.ConnID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), max(2*time.Minute, time.Duration(len(stmts))*20*time.Millisecond))
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeCursor()
	style := p.d.Style()
	if err := s.ensureConn(ctx, p); err != nil {
		return err
	}
	if err := s.setManual(ctx, style, true); err != nil {
		return err
	}
	if err := s.beginTx(ctx, style); err != nil {
		return err
	}
	save, undo, release := "SAVEPOINT tablume_apply", "ROLLBACK TO SAVEPOINT tablume_apply", "RELEASE SAVEPOINT tablume_apply"
	switch style {
	case "mssql":
		save, undo, release = "SAVE TRANSACTION tablume_apply", "ROLLBACK TRANSACTION tablume_apply", ""
	case "oracle":
		release = ""
	}
	if _, err := s.conn.ExecContext(ctx, save); err != nil {
		return err
	}
	for i, st := range stmts {
		res, err := s.conn.ExecContext(ctx, st.sql, st.args...)
		if err == nil && st.one {
			if n, rerr := res.RowsAffected(); rerr == nil && n != 1 {
				err = i18n.Errorf("%d번째 변경이 %d개 행에 적용되어 모두 취소했습니다. 다른 곳에서 바뀌었을 수 있으니 새로고침하세요.\n%s", i+1, n, inlineArgs(st.sql, st.args, p))
			}
		} else if err != nil {
			err = i18n.Errorf("%d번째 변경 실패 (모두 취소됨): %w\n%s", i+1, err, inlineArgs(st.sql, st.args, p))
		}
		if err != nil {
			_, _ = s.conn.ExecContext(ctx, undo)
			return err
		}
	}
	if release != "" {
		_, _ = s.conn.ExecContext(ctx, release)
	}
	return nil
}
