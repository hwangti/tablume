package dbx

import (
	"strings"
	"unicode/utf8"
)

// Stmt is one statement found by Split. Start/End are byte offsets of the
// trimmed statement text; Stop is the offset just past its delimiter.
type Stmt struct {
	SQL               string
	Start, End, Stop int
}

// Split breaks a script into statements the way the engine's usual CLI would.
//
//   - Quotes, quoted identifiers and comments never split.
//   - ';' ends a statement, except inside BEGIN ... END / CASE ... END blocks
//     (mysql, mssql, sqlite), so procedure bodies work without DELIMITER.
//   - mysql: "DELIMITER xx" lines are honoured too.
//   - oracle: PL/SQL units (BEGIN, DECLARE, CREATE PROCEDURE/FUNCTION/PACKAGE/
//     TRIGGER/TYPE) run until a line holding only "/".
//   - mssql: "GO" lines end a batch; CREATE/ALTER PROC/FUNCTION/TRIGGER run
//     until GO.
//   - postgres: $tag$ dollar quoting.
func Split(text, style string) []Stmt {
	s := splitter{text: text, style: style, delim: ";"}
	s.run()
	return s.out
}

type splitter struct {
	text  string
	style string
	delim string
	out   []Stmt

	start      int  // start of current statement
	hasContent bool // saw something other than whitespace/comments
	depth      int  // BEGIN/CASE nesting
	words      []string
	untilLine  bool // oracle PL/SQL or mssql module: only "/" or GO ends it
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '$' || c == '#'
}

func (s *splitter) emit(end, stop int) {
	if s.hasContent {
		raw := s.text[s.start:end]
		lead := len(raw) - len(strings.TrimLeft(raw, " \t\r\n"))
		trimmed := strings.TrimSpace(raw)
		st := s.start + lead
		s.out = append(s.out, Stmt{SQL: trimmed, Start: st, End: st + len(trimmed), Stop: stop})
	}
	s.start = stop
	s.hasContent = false
	s.depth = 0
	s.words = s.words[:0]
	s.untilLine = false
}

// lineCommand checks for a line-level command at a line start: "/" (oracle),
// "GO" (mssql) or "DELIMITER x" (mysql). Returns the offset after the line.
func (s *splitter) lineCommand(i int) (bool, int) {
	eol := strings.IndexByte(s.text[i:], '\n')
	next := len(s.text)
	if eol >= 0 {
		next = i + eol + 1
	}
	line := strings.TrimSpace(s.text[i:min(next, len(s.text))])
	switch s.style {
	case "oracle":
		if line == "/" {
			s.emit(i, next)
			return true, next
		}
	case "mssql":
		if strings.EqualFold(line, "GO") {
			s.emit(i, next)
			return true, next
		}
	case "mysql":
		if len(line) > 10 && strings.EqualFold(line[:10], "DELIMITER ") {
			s.emit(i, next)
			s.delim = strings.TrimSpace(line[10:])
			return true, next
		}
	}
	return false, i
}

// nextWord returns the upper-cased next word after position i, skipping
// whitespace and comments. Returns "" if the next token isn't a word.
func (s *splitter) nextWord(i int) string {
	t := s.text
	for i < len(t) {
		c := t[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '-' && i+1 < len(t) && t[i+1] == '-':
			e := strings.IndexByte(t[i:], '\n')
			if e < 0 {
				return ""
			}
			i += e
		case c == '/' && i+1 < len(t) && t[i+1] == '*':
			e := strings.Index(t[i+2:], "*/")
			if e < 0 {
				return ""
			}
			i += e + 4
		case isIdentStart(c):
			j := i
			for j < len(t) && isIdentChar(t[j]) {
				j++
			}
			return strings.ToUpper(t[i:j])
		default:
			return ""
		}
	}
	return ""
}

var txnBeginFollowers = map[string]bool{
	"": true, "TRAN": true, "TRANSACTION": true, "WORK": true, "DISTRIBUTED": true,
	"DEFERRED": true, "IMMEDIATE": true, "EXCLUSIVE": true, "DIALOG": true, "CONVERSATION": true,
}

var endNoClose = map[string]bool{"IF": true, "LOOP": true, "WHILE": true, "REPEAT": true, "CONVERSATION": true}

func (s *splitter) word(w string, after int) {
	u := strings.ToUpper(w)
	if len(s.words) < 6 {
		s.words = append(s.words, u)
		s.checkModule()
	}
	if s.style == "postgres" || s.style == "oracle" {
		return
	}
	switch u {
	case "BEGIN":
		if !txnBeginFollowers[s.nextWord(after)] {
			s.depth++
		}
	case "CASE":
		s.depth++
	case "END":
		if !endNoClose[s.nextWord(after)] && s.depth > 0 {
			s.depth--
		}
	}
}

func (s *splitter) checkModule() {
	w := s.words
	if s.untilLine || len(w) == 0 {
		return
	}
	switch s.style {
	case "oracle":
		if w[0] == "BEGIN" || w[0] == "DECLARE" {
			s.untilLine = true
			return
		}
		if w[0] != "CREATE" {
			return
		}
		for _, x := range w[1:] {
			switch x {
			case "OR", "REPLACE", "EDITIONABLE", "NONEDITIONABLE", "EDITIONING":
				continue
			case "PROCEDURE", "FUNCTION", "PACKAGE", "TRIGGER", "TYPE", "LIBRARY":
				s.untilLine = true
			}
			return
		}
	case "mssql":
		if w[0] != "CREATE" && w[0] != "ALTER" {
			return
		}
		for _, x := range w[1:] {
			switch x {
			case "OR", "ALTER":
				continue
			case "PROC", "PROCEDURE", "FUNCTION", "TRIGGER":
				s.untilLine = true
			}
			return
		}
	}
}

func (s *splitter) run() {
	t := s.text
	i := 0
	lineStart := true
	for i < len(t) {
		if lineStart {
			lineStart = false
			if ok, next := s.lineCommand(i); ok {
				i = next
				lineStart = true
				continue
			}
		}
		c := t[i]
		switch {
		case c == '\n':
			i++
			lineStart = true
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '-' && i+1 < len(t) && t[i+1] == '-',
			c == '#' && s.style == "mysql":
			e := strings.IndexByte(t[i:], '\n')
			if e < 0 {
				i = len(t)
			} else {
				i += e // the '\n' itself is handled next round
			}
		case c == '/' && i+1 < len(t) && t[i+1] == '*':
			e := strings.Index(t[i+2:], "*/")
			if e < 0 {
				i = len(t)
			} else {
				i += e + 4
			}
		case s.delim != ";" && strings.HasPrefix(t[i:], s.delim),
			c == ';' && s.delim == ";" && s.depth == 0 && !s.untilLine:
			s.emit(i, i+len(s.delim))
			i += len(s.delim)
		case c == '\'':
			s.hasContent = true
			i = s.skipQuoted(i+1, '\'', s.style == "mysql")
		case c == '"':
			s.hasContent = true
			i = s.skipQuoted(i+1, '"', s.style == "mysql")
		case c == '`' && s.style == "mysql":
			s.hasContent = true
			i = s.skipQuoted(i+1, '`', false)
		case c == '[' && s.style == "mssql":
			s.hasContent = true
			i = s.skipQuoted(i+1, ']', false)
		case c == '$' && s.style == "postgres":
			s.hasContent = true
			i = s.skipDollar(i)
		case (c == 'q' || c == 'Q') && s.style == "oracle" && i+2 < len(t) && t[i+1] == '\'' && (i == 0 || !isIdentChar(t[i-1])):
			s.hasContent = true
			i = s.skipOracleQ(i + 2)
		case isIdentStart(c):
			s.hasContent = true
			j := i
			for j < len(t) && isIdentChar(t[j]) && (s.delim == ";" || !strings.HasPrefix(t[j:], s.delim)) {
				j++
			}
			s.word(t[i:j], j)
			i = j
		default:
			s.hasContent = true
			_, size := utf8.DecodeRuneInString(t[i:])
			i += size
		}
	}
	s.emit(len(t), len(t))
}

func (s *splitter) skipQuoted(i int, q byte, backslash bool) int {
	t := s.text
	for i < len(t) {
		c := t[i]
		if backslash && c == '\\' {
			i += 2
			continue
		}
		if c == q {
			if i+1 < len(t) && t[i+1] == q {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return len(t)
}

func (s *splitter) skipDollar(i int) int {
	t := s.text
	j := i + 1
	for j < len(t) && (isIdentChar(t[j]) && t[j] != '$') {
		j++
	}
	if j >= len(t) || t[j] != '$' {
		return i + 1 // positional parameter like $1
	}
	if j > i+1 && t[i+1] >= '0' && t[i+1] <= '9' {
		return i + 1
	}
	tag := t[i : j+1]
	e := strings.Index(t[j+1:], tag)
	if e < 0 {
		return len(t)
	}
	return j + 1 + e + len(tag)
}

func (s *splitter) skipOracleQ(i int) int {
	t := s.text
	if i >= len(t) {
		return i
	}
	closer := t[i]
	switch closer {
	case '[':
		closer = ']'
	case '{':
		closer = '}'
	case '(':
		closer = ')'
	case '<':
		closer = '>'
	}
	e := strings.Index(t[i+1:], string(closer)+"'")
	if e < 0 {
		return len(t)
	}
	return i + 1 + e + 2
}

// utf16Len counts UTF-16 code units, which is what JavaScript offsets use.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// byteOffset converts a UTF-16 offset into a byte offset in s.
func byteOffset(s string, u16 int) int {
	n := 0
	for i, r := range s {
		if n >= u16 {
			return i
		}
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return len(s)
}

// PickAt returns the statement that the caret at byte offset pos belongs to.
// In the gap between two statements, the previous one wins if the caret is
// still on its last line; otherwise the next one does.
func PickAt(text string, stmts []Stmt, pos int) (Stmt, bool) {
	if len(stmts) == 0 {
		return Stmt{}, false
	}
	for k, st := range stmts {
		if pos >= st.Start && pos <= st.Stop {
			return st, true
		}
		if pos < st.Start {
			if k == 0 {
				return st, true
			}
			prev := stmts[k-1]
			if !strings.Contains(text[prev.Stop:pos], "\n") {
				return prev, true
			}
			return st, true
		}
	}
	return stmts[len(stmts)-1], true
}
