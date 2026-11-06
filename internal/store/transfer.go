package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"tablume/internal/dbx"
	"tablume/internal/i18n"
)

// Moving connections between installs: a JSON file with every setting but
// the passwords (and the SSH password or key passphrase), which stay in this
// install's keychain.

const exportFormat = "tablume-connections"

type exportFile struct {
	Format      string           `json:"format"`
	Version     int              `json:"version"`
	Connections []dbx.ConnConfig `json:"connections"`
}

// ImportResult counts what Import did with the file's connections.
type ImportResult struct {
	Added       int      `json:"added"`
	Existing    int      `json:"existing"`    // the same as one already saved
	Unsupported []string `json:"unsupported"` // their drivers aren't in this build
}

// Export writes the connections with these ids (all of them when none),
// in the saved order.
func (s *Store) Export(ids []string) ([]byte, error) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	conns := []map[string]any{}
	for _, c := range s.List() {
		if len(ids) > 0 && !want[c.ID] {
			continue
		}
		// as a map, to leave out what belongs to this install
		var m map[string]any
		b, _ := json.Marshal(c)
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		for _, k := range []string{"id", "password", "sshPassword", "savePassword"} {
			delete(m, k)
		}
		conns = append(conns, m)
	}
	return json.MarshalIndent(struct {
		Format      string           `json:"format"`
		Version     int              `json:"version"`
		Connections []map[string]any `json:"connections"`
	}{exportFormat, 1, conns}, "", "  ")
}

// Import adds the file's connections after the saved ones. It takes an
// export, or a copy of connections.json. One that matches a saved connection
// (same name, database and server, SSH included) isn't added again. Without
// a password, the app asks for it on connecting.
func (s *Store) Import(b []byte, supported []string) (ImportResult, error) {
	var res ImportResult
	var list []dbx.ConnConfig
	if b = bytes.TrimSpace(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))); len(b) > 0 && b[0] == '[' {
		if err := json.Unmarshal(b, &list); err != nil {
			return res, i18n.Errorf("연결 파일을 읽지 못했습니다: %v", err)
		}
	} else {
		var f exportFile
		if err := json.Unmarshal(b, &f); err != nil {
			return res, i18n.Errorf("연결 파일을 읽지 못했습니다: %v", err)
		}
		if f.Format != exportFormat {
			return res, i18n.Error("Tablume 연결 파일이 아닙니다")
		}
		if f.Version > 1 {
			return res, i18n.Error("더 새 버전의 Tablume에서 내보낸 파일입니다")
		}
		list = f.Connections
	}
	ok := map[string]bool{}
	for _, d := range supported {
		ok[d] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	for _, r := range s.list {
		seen[sameKey(r.ConnConfig)] = true
	}
	added := 0
	for _, c := range list {
		if c.Driver == "" {
			continue
		}
		if !ok[c.Driver] {
			res.Unsupported = append(res.Unsupported, c.Name)
			continue
		}
		if c.Name = strings.TrimSpace(c.Name); c.Name == "" {
			c.Name = c.Host
		}
		k := sameKey(c)
		if seen[k] {
			res.Existing++
			continue
		}
		seen[k] = true
		c.ID = dbx.NewID()
		c.Password, c.SSHPassword = "", ""
		c.SavePassword = false
		s.list = append(s.list, record{ConnConfig: c})
		added++
	}
	if added > 0 {
		if err := s.save(); err != nil {
			s.list = s.list[:len(s.list)-added]
			return ImportResult{}, err
		}
	}
	res.Added = added
	return res, nil
}

// What makes two connections the same one.
func sameKey(c dbx.ConnConfig) string {
	k := fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%v", c.Name, c.Driver, strings.ToLower(c.Host), c.Port, c.User, c.Database, c.OracleSID)
	if c.SSH {
		k += fmt.Sprintf("\x00%s\x00%d\x00%s", strings.ToLower(c.SSHHost), c.SSHPort, c.SSHUser)
	}
	return k
}
