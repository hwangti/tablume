package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The run log: what was run on a connection (console statements, grid
// edits, structure changes), kept per connection in logs/<id>.jsonl so it
// outlives the tab and the app.

type LogEntry struct {
	Time int64  `json:"time"` // Unix milliseconds
	Tab  string `json:"tab"`  // the tab's title then
	Kind string `json:"kind"` // info, error
	Text string `json:"text"` // the outcome: rows, time, the error
	SQL  string `json:"sql"`
}

const (
	logMaxBytes = 4 << 20 // past this the file is cut back to logKeep entries
	logKeep     = 3000
)

var logMu sync.Mutex // one writer at a time; files are small

func (s *Store) logPath(id string) (string, error) {
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return "", errors.New("bad connection id")
	}
	return filepath.Join(s.dir, "logs", id+".jsonl"), nil
}

// AppendLog adds entries to a connection's run log.
func (s *Store) AppendLog(id string, entries []LogEntry) error {
	p, err := s.logPath(id)
	if err != nil || len(entries) == 0 {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	logMu.Lock()
	defer logMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(buf.Bytes())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if st, serr := os.Stat(p); err == nil && serr == nil && st.Size() > logMaxBytes {
		err = trimLog(p)
	}
	return err
}

func trimLog(p string) error {
	data, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	lines := bytes.SplitAfter(data, []byte("\n"))
	if len(lines) > logKeep {
		lines = lines[len(lines)-logKeep:]
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, bytes.Join(lines, nil), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// ReadLog returns a connection's run log, newest first: up to limit
// entries whose SQL, outcome or tab contains q (case-insensitive).
func (s *Store) ReadLog(id, q string, limit int) ([]LogEntry, error) {
	p, err := s.logPath(id)
	if err != nil {
		return nil, err
	}
	logMu.Lock()
	data, err := os.ReadFile(p)
	logMu.Unlock()
	if errors.Is(err, os.ErrNotExist) {
		return []LogEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var all []LogEntry
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), logMaxBytes)
	for sc.Scan() {
		var e LogEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			all = append(all, e)
		}
	}
	q = strings.ToLower(strings.TrimSpace(q))
	out := []LogEntry{}
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		e := all[i]
		if q == "" || strings.Contains(strings.ToLower(e.SQL+"\n"+e.Text+"\n"+e.Tab), q) {
			out = append(out, e)
		}
	}
	return out, nil
}

// ClearLog deletes a connection's run log.
func (s *Store) ClearLog(id string) error {
	p, err := s.logPath(id)
	if err != nil {
		return err
	}
	logMu.Lock()
	defer logMu.Unlock()
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
