// Package store keeps saved connections in a JSON file in the user config
// directory. Passwords go to the OS keychain (macOS Keychain, Windows
// Credential Manager, Secret Service on Linux) and never into the file,
// unless no keychain is available.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"tablume/internal/dbx"

	"github.com/zalando/go-keyring"
)

const service = "Tablume"

type record struct {
	dbx.ConnConfig
	// FallbackPassword is only used when the OS keychain is unavailable.
	FallbackPassword string `json:"fallbackPassword,omitempty"`
}

type Store struct {
	mu   sync.Mutex
	dir  string
	list []record
}

func Dir() (string, error) {
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
				if pw, err := keyring.Get(service, id); err == nil {
					c.Password = pw
				} else {
					c.Password = r.FallbackPassword
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
	if idx >= 0 {
		rec.FallbackPassword = s.list[idx].FallbackPassword
	}
	switch {
	case !c.SavePassword:
		_ = keyring.Delete(service, c.ID)
		rec.FallbackPassword = ""
	case c.Password != "":
		if err := keyring.Set(service, c.ID, c.Password); err != nil {
			rec.FallbackPassword = c.Password
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
			_ = keyring.Delete(service, id)
			return s.save()
		}
	}
	return nil
}
