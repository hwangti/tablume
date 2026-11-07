package store

import (
	"fmt"
	"os"
	"testing"
)

func TestRunLog(t *testing.T) {
	s := &Store{dir: t.TempDir()}
	if got, err := s.ReadLog("c1", "", 10); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	if err := s.AppendLog("c1", []LogEntry{{Time: 1, SQL: "SELECT 1", Text: "1행"}, {Time: 2, SQL: "UPDATE t SET a = 1", Text: "3행 영향", Tab: "콘솔"}}); err != nil {
		t.Fatal(err)
	}
	s.AppendLog("c2", []LogEntry{{Time: 3, SQL: "SELECT 2"}})
	got, _ := s.ReadLog("c1", "", 10)
	if len(got) != 2 || got[0].Time != 2 {
		t.Fatalf("newest first: %+v", got)
	}
	if got, _ = s.ReadLog("c1", "update", 10); len(got) != 1 || got[0].Tab != "콘솔" {
		t.Fatalf("search: %+v", got)
	}
	if got, _ = s.ReadLog("c1", "", 1); len(got) != 1 {
		t.Fatalf("limit: %+v", got)
	}
	if _, err := s.ReadLog("../x", "", 1); err == nil {
		t.Fatal("path id accepted")
	}

	// cut back to logKeep entries past logMaxBytes
	big := make([]LogEntry, 0, 2000)
	pad := string(make([]byte, 1000))
	for i := 0; i < 5000; i++ {
		big = append(big, LogEntry{Time: int64(i), SQL: fmt.Sprintf("SELECT %d -- %q", i, pad)})
		if len(big) == cap(big) {
			s.AppendLog("c3", big)
			big = big[:0]
		}
	}
	s.AppendLog("c3", big)
	got, _ = s.ReadLog("c3", "", 100000)
	if len(got) > logKeep+2000 || got[0].Time != 4999 {
		t.Fatalf("trim: %d entries, newest %d", len(got), got[0].Time)
	}
	if err := s.ClearLog("c1"); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.ReadLog("c1", "", 10); len(got) != 0 {
		t.Fatal("not cleared")
	}
	p, _ := s.logPath("c2")
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}
