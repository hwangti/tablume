package dbx

import (
	"context"
	"database/sql/driver"

	"tablume/internal/i18n"
)

// Manual commit: a console tab keeps its changes in an open transaction
// until Commit or Rollback. MySQL/MariaDB (autocommit=0) and SQL Server
// (IMPLICIT_TRANSACTIONS) have a session switch for it; PostgreSQL and
// SQLite get a BEGIN before the first change; Oracle's driver commits every
// statement unless a transaction is open through it, so one is begun there.

// manualSwitch is the session setting that turns autocommit off (on) or
// back on, where the engine has one.
func manualSwitch(style string, on bool) string {
	switch style {
	case "mysql":
		if on {
			return "SET autocommit = 0"
		}
		return "SET autocommit = 1"
	case "mssql":
		if on {
			return "SET IMPLICIT_TRANSACTIONS ON"
		}
		return "SET IMPLICIT_TRANSACTIONS OFF"
	}
	return ""
}

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
	if q := manualSwitch(style, on); q != "" {
		if _, err := s.conn.ExecContext(ctx, q); err != nil {
			return err
		}
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
	switch style {
	case "postgres", "sqlite":
		if _, err := s.conn.ExecContext(ctx, "BEGIN"); err != nil {
			return err
		}
	case "oracle":
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

// afterStmt follows a COMMIT, ROLLBACK or BEGIN the user ran themselves.
func (s *session) afterStmt(style, stmt string) {
	if !s.manual {
		return
	}
	ws := sqlWords(stmt, style)
	if len(ws) == 0 {
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
		q = "IF @@TRANCOUNT > 0 " + q
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
