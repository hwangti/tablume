package dbx

import (
	"fmt"
	"strings"
)

// Results are read one page per query: the statement gets a LIMIT/OFFSET
// (or OFFSET … FETCH) clause, runs to completion and its cursor closes.
// Keeping a cursor open instead holds the table's metadata lock (ALTER
// TABLE elsewhere waits, and on MySQL/PostgreSQL everything queues behind
// that ALTER), and MySQL drops a result nobody reads for net_write_timeout.

// Words that make appending a page clause wrong or impossible, at the top
// level of the statement.
var notPageable = map[string]bool{
	"LIMIT": true, "OFFSET": true, "FETCH": true, "TOP": true, "INTO": true, "FOR": true,
	"LOCK": true, "PROCEDURE": true, "INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true,
}

var dmlWords = map[string]bool{"INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true}

type pageShape struct {
	ok       bool
	orderAt  int // byte offset of the top-level ORDER BY, or -1
	distinct bool
}

func shapeOf(stmt, style string) pageShape {
	toks := lexSQL(stmt, style)
	if len(toks) == 0 || toks[0].kind != 'w' {
		return pageShape{}
	}
	if first := strings.ToUpper(toks[0].text); first != "SELECT" && first != "WITH" {
		return pageShape{}
	}
	sh := pageShape{ok: true, orderAt: -1}
	for i, t := range toks {
		if t.depth != 0 {
			// a data-modifying CTE (PostgreSQL) must run exactly once
			if t.kind == 'w' && dmlWords[strings.ToUpper(t.text)] {
				return pageShape{}
			}
			continue
		}
		if t.kind == 'p' && t.text == ";" && i != len(toks)-1 {
			return pageShape{}
		}
		if t.kind != 'w' {
			continue
		}
		u := strings.ToUpper(t.text)
		if notPageable[u] {
			return pageShape{}
		}
		if u == "DISTINCT" || u == "UNION" || u == "INTERSECT" || u == "EXCEPT" {
			sh.distinct = true // SQL Server: ORDER BY must use select-list items
		}
		if u == "ORDER" && i+1 < len(toks) && strings.EqualFold(toks[i+1].text, "BY") {
			sh.orderAt = t.pos
		}
	}
	return sh
}

func trimStmt(stmt string) string {
	return strings.TrimRight(strings.TrimSpace(stmt), "; \t\r\n")
}

// pagedSQL returns stmt limited to n rows after skipping offset, or false
// when the statement can't take a page clause (not a plain query, or it
// already limits itself).
func pagedSQL(stmt, style string, n, offset int, legacyOracle bool) (string, bool) {
	sh := shapeOf(stmt, style)
	if !sh.ok || (style == "oracle" && legacyOracle) {
		return "", false
	}
	// On a new line: the statement may end with a -- comment.
	q := trimStmt(stmt) + "\n"
	switch style {
	case "mssql":
		if sh.orderAt < 0 {
			if sh.distinct {
				q += "ORDER BY 1\n"
			} else {
				q += "ORDER BY (SELECT NULL)\n"
			}
		}
		return q + fmt.Sprintf("OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", offset, n), true
	case "oracle": // 12c+
		if offset > 0 {
			return q + fmt.Sprintf("OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", offset, n), true
		}
		return q + fmt.Sprintf("FETCH FIRST %d ROWS ONLY", n), true
	}
	if offset > 0 {
		return q + fmt.Sprintf("LIMIT %d OFFSET %d", n, offset), true
	}
	return q + fmt.Sprintf("LIMIT %d", n), true
}

// countSQL counts the rows a query returns (its ORDER BY dropped: derived
// tables can't have one on SQL Server, and it doesn't change the count).
func countSQL(stmt, style string) string {
	q := trimStmt(stmt)
	if sh := shapeOf(q, style); sh.orderAt >= 0 {
		q = q[:sh.orderAt]
	}
	return "SELECT COUNT(*) FROM (\n" + q + "\n) tablume_count"
}

// isLegacyOracle: before 12c there is no OFFSET … FETCH.
func isLegacyOracle(version string) bool {
	for _, v := range []string{"8i", "9i", "10g", "11g", "Release 8.", "Release 9.", "Release 10.", "Release 11."} {
		if strings.Contains(version, v) {
			return true
		}
	}
	return false
}
