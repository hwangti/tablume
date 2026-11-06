package dbx

import (
	"strings"
	"unicode/utf8"

	"tablume/internal/i18n"
)

// Safeguards for production databases: read-only connections, and a
// confirmation before UPDATE/DELETE without WHERE.

// writable refuses changes on a read-only connection.
func (p *pool) writable() error {
	if p.cfg.ReadOnly {
		return i18n.Error("읽기 전용 연결입니다")
	}
	return nil
}

type sqlWord struct {
	w     string // upper case
	depth int    // parenthesis depth
}

// sqlWords lists a statement's words outside strings, quoted identifiers
// and comments, with how deep in parentheses each is.
func sqlWords(stmt, style string) []sqlWord {
	s := splitter{text: stmt, style: style}
	t := stmt
	var out []sqlWord
	depth := 0
	for i := 0; i < len(t); {
		c := t[i]
		switch {
		case c == '-' && i+1 < len(t) && t[i+1] == '-', c == '#' && style == "mysql":
			e := strings.IndexByte(t[i:], '\n')
			if e < 0 {
				return out
			}
			i += e
		case c == '/' && i+1 < len(t) && t[i+1] == '*':
			e := strings.Index(t[i+2:], "*/")
			if e < 0 {
				return out
			}
			i += e + 4
		case c == '\'' || c == '"':
			i = s.skipQuoted(i+1, c, style == "mysql")
		case c == '`' && style == "mysql":
			i = s.skipQuoted(i+1, '`', false)
		case c == '[' && style == "mssql":
			i = s.skipQuoted(i+1, ']', false)
		case c == '$' && style == "postgres":
			i = s.skipDollar(i)
		case (c == 'q' || c == 'Q') && style == "oracle" && i+2 < len(t) && t[i+1] == '\'' && (i == 0 || !isIdentChar(t[i-1])):
			i = s.skipOracleQ(i + 2)
		case c == '(':
			depth++
			i++
		case c == ')':
			depth--
			i++
		case isIdentStart(c):
			j := i
			for j < len(t) && isIdentChar(t[j]) {
				j++
			}
			out = append(out, sqlWord{strings.ToUpper(t[i:j]), depth})
			i = j
		default:
			_, size := utf8.DecodeRuneInString(t[i:])
			i += size
		}
	}
	return out
}

// verbAt finds the statement's verb: the first word, or after a WITH
// clause the first top-level SELECT/INSERT/UPDATE/DELETE/MERGE. Returns
// its index, -1 if none.
func verbAt(ws []sqlWord) int {
	if len(ws) == 0 {
		return -1
	}
	if ws[0].w != "WITH" {
		return 0
	}
	for i, x := range ws[1:] {
		switch x.w {
		case "SELECT", "INSERT", "UPDATE", "DELETE", "MERGE":
			if x.depth == ws[0].depth {
				return i + 1
			}
		}
	}
	return -1
}

// noWhere: an UPDATE or DELETE without a WHERE of its own, which changes
// every row of the table.
func noWhere(stmt, style string) bool {
	ws := sqlWords(stmt, style)
	v := verbAt(ws)
	if v < 0 || (ws[v].w != "UPDATE" && ws[v].w != "DELETE") {
		return false
	}
	for _, x := range ws[v+1:] {
		if x.w == "WHERE" && x.depth == ws[v].depth {
			return false
		}
	}
	return true
}

var readVerbs = map[string]bool{
	"SELECT": true, "WITH": true, "VALUES": true, "TABLE": true, "SHOW": true,
	"DESC": true, "DESCRIBE": true, "EXPLAIN": true, "HELP": true, "USE": true, "PRAGMA": true,
}

// Words that make a reading statement write: data changes inside a WITH
// (PostgreSQL), SELECT … INTO (a new table, a file), FOR UPDATE locks,
// EXPLAIN ANALYZE running a DML.
var writeWords = map[string]bool{"INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true, "INTO": true, "UPSERT": true}

// readsOnly: the statement only reads, as far as its words tell. A
// read-only connection runs only these; MySQL/MariaDB, PostgreSQL and
// SQLite sessions are also read-only in the database itself, which
// catches functions that write.
func readsOnly(stmt, style string) bool {
	ws := sqlWords(stmt, style)
	if len(ws) == 0 {
		return true // comments only
	}
	if !readVerbs[ws[0].w] {
		return false
	}
	for _, x := range ws {
		if writeWords[x.w] {
			return false
		}
	}
	// PRAGMA x = v sets a value (query_only = 0 would undo the guard)
	if ws[0].w == "PRAGMA" && (strings.Contains(stmt, "=") || strings.Contains(strings.ToUpper(stmt), "QUERY_ONLY")) {
		return false
	}
	return true
}

// firstLine: a statement's first line, for messages.
func firstLine(s string) string {
	s = strings.TrimSpace(stripLeadingComments(s))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + " …"
	}
	if utf8.RuneCountInString(s) > 80 {
		s = string([]rune(s)[:80]) + "…"
	}
	return s
}
