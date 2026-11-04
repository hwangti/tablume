// Package store keeps saved connections in a JSON file in the user config
// directory. Passwords go to the OS keychain (macOS Keychain, Windows
// Credential Manager, Secret Service on Linux) and never into the file,
// unless no keychain is available (or, in the browser build, isn't used):
// then they are stored in the file encrypted with a key kept beside it.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"tablume/internal/dbx"

	"github.com/zalando/go-keyring"
)

const service = "Tablume"

type record struct {
	dbx.ConnConfig
	// FallbackPassword is only used when the OS keychain is unavailable.
	FallbackPassword    string `json:"fallbackPassword,omitempty"`
	FallbackSSHPassword string `json:"fallbackSshPassword,omitempty"`
}

// The SSH password / key passphrase has its own keychain entry.
func sshKey(id string) string { return id + "/ssh" }

type Store struct {
	mu   sync.Mutex
	dir  string
	list []record
}

var (
	// DirOverride replaces the config directory (browser build: -data).
	DirOverride string
	// NoKeyring keeps passwords out of the OS keychain (a server usually
	// has none, and the data directory should be all there is to back up).
	NoKeyring bool
)

func keyringGet(id string) (string, error) {
	if NoKeyring {
		return "", keyring.ErrNotFound
	}
	return keyring.Get(service, id)
}

func keyringSet(id, pw string) error {
	if NoKeyring {
		return keyring.ErrUnsupportedPlatform
	}
	return keyring.Set(service, id, pw)
}

func keyringDelete(id string) {
	if !NoKeyring {
		_ = keyring.Delete(service, id)
	}
}

func Dir() (string, error) {
	if DirOverride != "" {
		return DirOverride, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Tablume"), nil
}

func Open() (*Store, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir}
	b, err := os.ReadFile(s.path())
	if err == nil {
		if err := json.Unmarshal(b, &s.list); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s, nil
}

func (s *Store) path() string { return filepath.Join(s.dir, "connections.json") }

func (s *Store) save() error {
	b, err := json.MarshalIndent(s.list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path())
}

// List returns saved connections without passwords.
func (s *Store) List() []dbx.ConnConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]dbx.ConnConfig, len(s.list))
	for i, r := range s.list {
		out[i] = r.ConnConfig
		out[i].Password = ""
		out[i].SSHPassword = ""
	}
	return out
}

// Get returns a connection including its stored password.
func (s *Store) Get(id string) (dbx.ConnConfig, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.list {
		if r.ID == id {
			c := r.ConnConfig
			if c.SavePassword {
				if pw, err := keyringGet(id); err == nil {
					c.Password = pw
				} else {
					c.Password = s.reveal(r.FallbackPassword)
				}
			}
			if c.SSH {
				if pw, err := keyringGet(sshKey(id)); err == nil {
					c.SSHPassword = pw
				} else {
					c.SSHPassword = s.reveal(r.FallbackSSHPassword)
				}
			}
			return c, true
		}
	}
	return dbx.ConnConfig{}, false
}

// Save inserts or updates a connection. An empty password on an existing
// connection keeps the stored one.
func (s *Store) Save(c dbx.ConnConfig) (dbx.ConnConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == "" {
		c.ID = dbx.NewID()
	}
	idx := -1
	for i, r := range s.list {
		if r.ID == c.ID {
			idx = i
		}
	}
	rec := record{ConnConfig: c}
	rec.Password = ""
	rec.SSHPassword = ""
	if idx >= 0 {
		rec.FallbackPassword = s.list[idx].FallbackPassword
		rec.FallbackSSHPassword = s.list[idx].FallbackSSHPassword
	}
	switch {
	case !c.SSH:
		keyringDelete(sshKey(c.ID))
		rec.FallbackSSHPassword = ""
	case c.SSHPassword != "":
		if err := keyringSet(sshKey(c.ID), c.SSHPassword); err != nil {
			if rec.FallbackSSHPassword, err = s.seal(c.SSHPassword); err != nil {
				return dbx.ConnConfig{}, err
			}
		} else {
			rec.FallbackSSHPassword = ""
		}
	}
	switch {
	case !c.SavePassword:
		keyringDelete(c.ID)
		rec.FallbackPassword = ""
	case c.Password != "":
		if err := keyringSet(c.ID, c.Password); err != nil {
			if rec.FallbackPassword, err = s.seal(c.Password); err != nil {
				return dbx.ConnConfig{}, err
			}
		} else {
			rec.FallbackPassword = ""
		}
	}
	if idx >= 0 {
		s.list[idx] = rec
	} else {
		s.list = append(s.list, rec)
	}
	if err := s.save(); err != nil {
		return dbx.ConnConfig{}, err
	}
	out := rec.ConnConfig
	return out, nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.list {
		if r.ID == id {
			s.list = append(s.list[:i], s.list[i+1:]...)
			keyringDelete(id)
			keyringDelete(sshKey(id))
			return s.save()
		}
	}
	return nil
}

// SetSchemaFilter stores which schemas the tree shows for a connection.
func (s *Store) SetSchemaFilter(id string, f *dbx.SchemaFilter) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		if s.list[i].ID == id {
			s.list[i].Schemas = f
			return s.save()
		}
	}
	return nil
}

// Reorder puts the connections in the order of ids; ones not listed keep
// their relative order at the end.
func (s *Store) Reorder(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pos := make(map[string]int, len(ids))
	for i, id := range ids {
		pos[id] = i
	}
	rank := func(r record) int {
		if p, ok := pos[r.ID]; ok {
			return p
		}
		return len(ids)
	}
	sort.SliceStable(s.list, func(i, j int) bool { return rank(s.list[i]) < rank(s.list[j]) })
	return s.save()
}

// SetSSHHostKey pins the SSH server key seen on the first connection.
func (s *Store) SetSSHHostKey(id, fp string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		if s.list[i].ID == id {
			s.list[i].SSHHostKey = fp
			return s.save()
		}
	}
	return nil
}
