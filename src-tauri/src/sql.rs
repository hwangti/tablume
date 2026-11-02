//! SQL text handling for MySQL/MariaDB: splitting a script into statements,
//! finding the statement under the caret, the single source table of a
//! SELECT, and adding a page clause. A port of the Go app's split.go,
//! page.go and the lexer in edit.go, MySQL style only.

#[derive(Debug, Clone)]
pub struct Stmt {
    pub sql: String,
    /// Byte offsets of the trimmed statement text.
    pub start: usize,
    pub end: usize,
    /// Offset just past the statement's delimiter.
    pub stop: usize,
}

fn is_ident_start(c: u8) -> bool {
    c == b'_' || c.is_ascii_alphabetic() || c >= 0x80
}

fn is_ident_char(c: u8) -> bool {
    is_ident_start(c) || c.is_ascii_digit() || c == b'$' || c == b'#'
}

/// Skips a quoted string starting just after the opening quote; returns the
/// offset after the closing quote. Doubled quotes and (optionally)
/// backslash escapes stay inside.
fn skip_quoted(t: &[u8], mut i: usize, q: u8, backslash: bool) -> usize {
    while i < t.len() {
        let c = t[i];
        if backslash && c == b'\\' {
            i += 2;
            continue;
        }
        if c == q {
            if i + 1 < t.len() && t[i + 1] == q {
                i += 2;
                continue;
            }
            return i + 1;
        }
        i += 1;
    }
    t.len()
}

fn find(t: &[u8], from: usize, pat: &[u8]) -> Option<usize> {
    if from > t.len() {
        return None;
    }
    t[from..].windows(pat.len()).position(|w| w == pat).map(|p| from + p)
}

/// The upper-cased next word after position i, skipping whitespace and
/// comments; "" if the next token isn't a word.
fn next_word(text: &str, mut i: usize) -> String {
    let t = text.as_bytes();
    while i < t.len() {
        let c = t[i];
        if matches!(c, b' ' | b'\t' | b'\r' | b'\n') {
            i += 1;
        } else if c == b'-' && i + 1 < t.len() && t[i + 1] == b'-' {
            match find(t, i, b"\n") {
                Some(e) => i = e,
                None => return String::new(),
            }
        } else if c == b'/' && i + 1 < t.len() && t[i + 1] == b'*' {
            match find(t, i + 2, b"*/") {
                Some(e) => i = e + 2,
                None => return String::new(),
            }
        } else if is_ident_start(c) {
            let mut j = i;
            while j < t.len() && is_ident_char(t[j]) {
                j += 1;
            }
            return text[i..j].to_uppercase();
        } else {
            return String::new();
        }
    }
    String::new()
}

const TXN_BEGIN_FOLLOWERS: &[&str] = &["", "TRAN", "TRANSACTION", "WORK", "DISTRIBUTED", "DEFERRED", "IMMEDIATE", "EXCLUSIVE"];
const END_NO_CLOSE: &[&str] = &["IF", "LOOP", "WHILE", "REPEAT"];

struct Splitter<'a> {
    text: &'a str,
    delim: String,
    out: Vec<Stmt>,
    start: usize,
    has_content: bool,
    depth: i32, // BEGIN/CASE nesting
}

impl<'a> Splitter<'a> {
    fn emit(&mut self, end: usize, stop: usize) {
        if self.has_content {
            let raw = &self.text[self.start..end];
            let lead = raw.len() - raw.trim_start_matches([' ', '\t', '\r', '\n']).len();
            let trimmed = raw.trim();
            let st = self.start + lead;
            self.out.push(Stmt { sql: trimmed.to_string(), start: st, end: st + trimmed.len(), stop });
        }
        self.start = stop;
        self.has_content = false;
        self.depth = 0;
    }

    /// "DELIMITER x" at a line start. Returns the offset after the line.
    fn line_command(&mut self, i: usize) -> Option<usize> {
        let t = self.text;
        let next = t[i..].find('\n').map(|e| i + e + 1).unwrap_or(t.len());
        let line = t[i..next].trim();
        if line.len() > 10 && line.is_char_boundary(10) && line[..10].eq_ignore_ascii_case("DELIMITER ") {
            self.emit(i, next);
            self.delim = line[10..].trim().to_string();
            return Some(next);
        }
        None
    }

    fn word(&mut self, w: &str, after: usize) {
        match w.to_uppercase().as_str() {
            "BEGIN" => {
                if !TXN_BEGIN_FOLLOWERS.contains(&next_word(self.text, after).as_str()) {
                    self.depth += 1;
                }
            }
            "CASE" => self.depth += 1,
            "END" => {
                if !END_NO_CLOSE.contains(&next_word(self.text, after).as_str()) && self.depth > 0 {
                    self.depth -= 1;
                }
            }
            _ => {}
        }
    }

    fn run(&mut self) {
        let t = self.text.as_bytes();
        let mut i = 0;
        let mut line_start = true;
        while i < t.len() {
            if line_start {
                line_start = false;
                if let Some(next) = self.line_command(i) {
                    i = next;
                    line_start = true;
                    continue;
                }
            }
            let c = t[i];
            let custom = self.delim != ";";
            if c == b'\n' {
                i += 1;
                line_start = true;
            } else if matches!(c, b' ' | b'\t' | b'\r') {
                i += 1;
            } else if (c == b'-' && i + 1 < t.len() && t[i + 1] == b'-') || c == b'#' {
                i = find(t, i, b"\n").unwrap_or(t.len()); // '\n' handled next round
            } else if c == b'/' && i + 1 < t.len() && t[i + 1] == b'*' {
                i = find(t, i + 2, b"*/").map(|e| e + 2).unwrap_or(t.len());
            } else if (custom && t[i..].starts_with(self.delim.as_bytes())) || (c == b';' && !custom && self.depth == 0) {
                let n = self.delim.len();
                self.emit(i, i + n);
                i += n;
            } else if c == b'\'' || c == b'"' {
                self.has_content = true;
                i = skip_quoted(t, i + 1, c, true);
            } else if c == b'`' {
                self.has_content = true;
                i = skip_quoted(t, i + 1, b'`', false);
            } else if is_ident_start(c) {
                self.has_content = true;
                let mut j = i;
                while j < t.len() && is_ident_char(t[j]) && (!custom || !t[j..].starts_with(self.delim.as_bytes())) {
                    j += 1;
                }
                let w = &self.text[i..j];
                self.word(w, j);
                i = j;
            } else {
                self.has_content = true;
                i += utf8_len(c);
            }
        }
        self.emit(t.len(), t.len());
    }
}

fn utf8_len(c: u8) -> usize {
    match c {
        0xF0..=0xFF => 4,
        0xE0..=0xEF => 3,
        0xC0..=0xDF => 2,
        _ => 1,
    }
}

/// Breaks a script into statements the way the mysql CLI would: quotes and
/// comments never split, ';' inside BEGIN … END / CASE … END doesn't either
/// (procedure bodies work without DELIMITER), and DELIMITER lines are honoured.
pub fn split(text: &str) -> Vec<Stmt> {
    let mut s = Splitter { text, delim: ";".into(), out: vec![], start: 0, has_content: false, depth: 0 };
    s.run();
    s.out
}

/// UTF-16 code units in s, which is what JavaScript offsets count.
pub fn utf16_len(s: &str) -> usize {
    s.chars().map(char::len_utf16).sum()
}

/// A UTF-16 offset into s as a byte offset.
pub fn byte_offset(s: &str, u16: usize) -> usize {
    let mut n = 0;
    for (i, ch) in s.char_indices() {
        if n >= u16 {
            return i;
        }
        n += ch.len_utf16();
    }
    s.len()
}

/// The statement the caret at byte offset pos belongs to. Between two
/// statements, the previous one wins while the caret is still on its last
/// line; otherwise the next one does.
pub fn pick_at(text: &str, stmts: &[Stmt], pos: usize) -> Option<Stmt> {
    for (k, st) in stmts.iter().enumerate() {
        if pos >= st.start && pos <= st.stop {
            return Some(st.clone());
        }
        if pos < st.start {
            if k == 0 {
                return Some(st.clone());
            }
            let prev = &stmts[k - 1];
            if !text[prev.stop..pos].contains('\n') {
                return Some(prev.clone());
            }
            return Some(st.clone());
        }
    }
    stmts.last().cloned()
}

// ---------------------------------------------------------------- tokens

#[derive(Debug, Clone, Copy, PartialEq)]
enum Kind {
    Word,
    Quoted, // quoted identifier
    Str,    // string literal
    Punct,
}

#[derive(Debug, Clone)]
struct Token {
    kind: Kind,
    text: String,
    depth: i32, // parenthesis depth
    pos: usize,
}

/// A small tokenizer, good enough to find the FROM clause.
fn lex(s: &str) -> Vec<Token> {
    let t = s.as_bytes();
    let mut out = vec![];
    let mut depth = 0;
    let mut i = 0;
    while i < t.len() {
        let c = t[i];
        if matches!(c, b' ' | b'\t' | b'\r' | b'\n') {
            i += 1;
        } else if (c == b'-' && i + 1 < t.len() && t[i + 1] == b'-') || c == b'#' {
            while i < t.len() && t[i] != b'\n' {
                i += 1;
            }
        } else if c == b'/' && i + 1 < t.len() && t[i + 1] == b'*' {
            match find(t, i + 2, b"*/") {
                Some(e) => i = e + 2,
                None => return out,
            }
        } else if c == b'\'' {
            let j = skip_quoted(t, i + 1, b'\'', true);
            out.push(Token { kind: Kind::Str, text: s[i..j].into(), depth, pos: i });
            i = j;
        } else if c == b'"' || c == b'`' {
            let j = skip_quoted(t, i + 1, c, false);
            let inner = &s[i + 1..(j.saturating_sub(1)).max(i + 1)];
            let q = (c as char).to_string();
            out.push(Token { kind: Kind::Quoted, text: inner.replace(&q.repeat(2), &q), depth, pos: i });
            i = j;
        } else if is_ident_start(c) {
            let mut j = i;
            while j < t.len() && is_ident_char(t[j]) {
                j += 1;
            }
            out.push(Token { kind: Kind::Word, text: s[i..j].into(), depth, pos: i });
            i = j;
        } else {
            if c == b'(' {
                depth += 1;
            }
            let n = utf8_len(c);
            out.push(Token { kind: Kind::Punct, text: s[i..i + n].into(), depth, pos: i });
            if c == b')' {
                depth -= 1;
            }
            i += n;
        }
    }
    out
}

fn is_word(t: &Token, w: &str) -> bool {
    t.kind == Kind::Word && t.text.eq_ignore_ascii_case(w)
}

const NOT_SINGLE_TABLE: &[&str] = &["JOIN", "UNION", "INTERSECT", "EXCEPT", "MINUS", "GROUP", "HAVING", "DISTINCT", "LATERAL", "INTO"];
const FROM_ENDERS: &[&str] = &["WHERE", "ORDER", "LIMIT", "FETCH", "OFFSET", "FOR", "WINDOW"];

/// The identifier parts of the single table a SELECT reads from, or None when
/// the query isn't a plain single-table SELECT.
pub fn detect_source(stmt: &str) -> Option<Vec<String>> {
    let toks = lex(stmt);
    if toks.is_empty() || !is_word(&toks[0], "SELECT") {
        return None;
    }
    let mut from = None;
    for (i, t) in toks.iter().enumerate() {
        if t.depth != 0 || t.kind != Kind::Word {
            continue;
        }
        let u = t.text.to_uppercase();
        if NOT_SINGLE_TABLE.contains(&u.as_str()) {
            return None;
        }
        if u == "FROM" && from.is_none() {
            from = Some(i);
        }
    }
    let mut i = from? + 1;
    let mut parts = vec![];
    loop {
        let t = toks.get(i)?;
        if !(t.kind == Kind::Word || t.kind == Kind::Quoted) || t.depth != 0 {
            return None;
        }
        parts.push(t.text.clone());
        i += 1;
        if toks.get(i).is_some_and(|t| t.kind == Kind::Punct && t.text == ".") {
            i += 1;
            continue;
        }
        break;
    }
    // optional alias
    if toks.get(i).is_some_and(|t| is_word(t, "AS")) {
        i += 1;
    }
    if let Some(t) = toks.get(i) {
        if t.kind == Kind::Quoted || (t.kind == Kind::Word && !FROM_ENDERS.contains(&t.text.to_uppercase().as_str())) {
            i += 1;
        }
    }
    if let Some(t) = toks.get(i) {
        if t.kind == Kind::Punct && t.text == ";" {
            return Some(parts);
        }
        if t.kind != Kind::Word || !FROM_ENDERS.contains(&t.text.to_uppercase().as_str()) {
            return None; // comma join etc.
        }
    }
    if parts.len() > 3 {
        return None;
    }
    Some(parts)
}

// ---------------------------------------------------------------- paging
// Results are read one page per query: the statement gets a LIMIT/OFFSET
// clause and runs to completion. An open cursor would hold the table's
// metadata lock, and MySQL drops a result nobody reads for net_write_timeout.

const NOT_PAGEABLE: &[&str] = &["LIMIT", "OFFSET", "FETCH", "INTO", "FOR", "LOCK", "PROCEDURE", "INSERT", "UPDATE", "DELETE"];

struct Shape {
    ok: bool,
    order_at: Option<usize>, // byte offset of the top-level ORDER BY
}

fn shape_of(stmt: &str) -> Shape {
    let no = Shape { ok: false, order_at: None };
    let toks = lex(stmt);
    let Some(first) = toks.first() else { return no };
    if !(is_word(first, "SELECT") || is_word(first, "WITH")) {
        return no;
    }
    let mut sh = Shape { ok: true, order_at: None };
    for (i, t) in toks.iter().enumerate() {
        if t.depth != 0 {
            continue;
        }
        if t.kind == Kind::Punct && t.text == ";" && i != toks.len() - 1 {
            return no;
        }
        if t.kind != Kind::Word {
            continue;
        }
        let u = t.text.to_uppercase();
        if NOT_PAGEABLE.contains(&u.as_str()) {
            return no;
        }
        if u == "ORDER" && toks.get(i + 1).is_some_and(|n| is_word(n, "BY")) {
            sh.order_at = Some(t.pos);
        }
    }
    sh
}

fn trim_stmt(stmt: &str) -> &str {
    stmt.trim().trim_end_matches([';', ' ', '\t', '\r', '\n'])
}

/// stmt limited to n rows after skipping offset, or None when it can't take
/// a page clause (not a plain query, or it already limits itself).
pub fn paged_sql(stmt: &str, n: usize, offset: usize) -> Option<String> {
    if !shape_of(stmt).ok {
        return None;
    }
    // On a new line: the statement may end with a -- comment.
    let q = format!("{}\n", trim_stmt(stmt));
    Some(if offset > 0 { format!("{q}LIMIT {n} OFFSET {offset}") } else { format!("{q}LIMIT {n}") })
}

/// Counts the rows a query returns (its ORDER BY dropped).
pub fn count_sql(stmt: &str) -> String {
    let mut q = trim_stmt(stmt);
    if let Some(at) = shape_of(q).order_at {
        q = &q[..at];
    }
    format!("SELECT COUNT(*) FROM (\n{q}\n) tablume_count")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn splits_statements() {
        let s = split("select 1; select ';' ; -- x;\nselect `a;b` from t");
        let v: Vec<_> = s.iter().map(|s| s.sql.as_str()).collect();
        assert_eq!(v, ["select 1", "select ';'", "-- x;\nselect `a;b` from t"]);
    }

    #[test]
    fn keeps_procedure_bodies() {
        let sql = "CREATE PROCEDURE p() BEGIN SELECT 1; IF 1 THEN SELECT 2; END IF; END;\nSELECT 3";
        assert_eq!(split(sql).len(), 2);
        let d = "DELIMITER $$\nCREATE PROCEDURE p() BEGIN SELECT 1; END$$\nDELIMITER ;\nSELECT 2;";
        let v: Vec<_> = split(d).into_iter().map(|s| s.sql).collect();
        assert_eq!(v, ["CREATE PROCEDURE p() BEGIN SELECT 1; END", "SELECT 2"]);
        assert_eq!(split("BEGIN; SELECT 1; COMMIT;").len(), 3);
    }

    #[test]
    fn picks_statement_at_caret() {
        let t = "select 1;\nselect 2;";
        let s = split(t);
        assert_eq!(pick_at(t, &s, 9).unwrap().sql, "select 1");
        assert_eq!(pick_at(t, &s, 10).unwrap().sql, "select 2");
        assert_eq!(utf16_len("한😀"), 3);
        assert_eq!(byte_offset("한😀a", 3), 7);
    }

    #[test]
    fn finds_source_table() {
        assert_eq!(detect_source("select * from shop.items i where id=1"), Some(vec!["shop".into(), "items".into()]));
        assert_eq!(detect_source("select * from `a``b`"), Some(vec!["a`b".into()]));
        assert_eq!(detect_source("select * from a join b"), None);
        assert_eq!(detect_source("select * from a, b"), None);
    }

    #[test]
    fn pages_queries() {
        assert_eq!(paged_sql("select * from t;", 10, 0).unwrap(), "select * from t\nLIMIT 10");
        assert_eq!(paged_sql("select * from t limit 5", 10, 0), None);
        assert_eq!(paged_sql("select (select 1 limit 1) x", 10, 20).unwrap(), "select (select 1 limit 1) x\nLIMIT 10 OFFSET 20");
        assert_eq!(count_sql("select * from t order by a"), "SELECT COUNT(*) FROM (\nselect * from t \n) tablume_count");
    }
}
