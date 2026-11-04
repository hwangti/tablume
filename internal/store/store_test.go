package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tablume/internal/dbx"
)

// Without a keychain, passwords are stored encrypted, not in plain text.
func TestPasswordsWithoutKeychain(t *testing.T) {
	NoKeyring = true
	DirOverride = t.TempDir()
	defer func() { NoKeyring, DirOverride = false, "" }()

	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Save(dbx.ConnConfig{Name: "x", Driver: "mysql", Password: "hunter2", SavePassword: true, SSH: true, SSHPassword: "pass phrase"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(DirOverride, "connections.json"))
	if strings.Contains(string(b), "hunter2") || strings.Contains(string(b), "pass phrase") {
		t.Fatalf("plain password in connections.json:\n%s", b)
	}
	s2, _ := Open()
	got, _ := s2.Get(c.ID)
	if got.Password != "hunter2" || got.SSHPassword != "pass phrase" {
		t.Fatalf("read back %q %q", got.Password, got.SSHPassword)
	}
	if l := s2.List(); l[0].Password != "" {
		t.Fatal("List returned the password")
	}

	// a file written before encryption still reads
	if v := s2.reveal("legacy"); v != "legacy" {
		t.Fatalf("legacy value: %q", v)
	}
	// without the key, the password is just gone (asked for again)
	os.Remove(filepath.Join(DirOverride, "secret.key"))
	if got, _ := s2.Get(c.ID); got.Password != "" {
		t.Fatalf("decrypted without key: %q", got.Password)
	}
}
