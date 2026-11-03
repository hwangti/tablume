package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Passwords that can't go to a keychain are kept in connections.json
// encrypted with AES-GCM under a random key in secret.key (same directory,
// owner-only). That keeps them out of a copied or synced connections file;
// anyone who can read the whole directory can still read them.

const sealPrefix = "enc1:"

func (s *Store) keyPath() string { return filepath.Join(s.dir, "secret.key") }

// caller holds mu
func (s *Store) aead(create bool) (cipher.AEAD, error) {
	key, err := os.ReadFile(s.keyPath())
	if errors.Is(err, os.ErrNotExist) && create {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.WriteFile(s.keyPath(), key, 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("secret.key 파일이 올바르지 않습니다")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (s *Store) seal(plain string) (string, error) {
	g, err := s.aead(true)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := g.Seal(nonce, nonce, []byte(plain), nil)
	return sealPrefix + base64.StdEncoding.EncodeToString(out), nil
}

// reveal decrypts a stored password. Values from before encryption are
// plain text and returned as they are; ones that no longer decrypt (the
// key file was lost) come back empty, so the app asks again.
func (s *Store) reveal(v string) string {
	if !strings.HasPrefix(v, sealPrefix) {
		return v
	}
	b, err := base64.StdEncoding.DecodeString(v[len(sealPrefix):])
	if err != nil {
		return ""
	}
	g, err := s.aead(false)
	if err != nil || len(b) < g.NonceSize() {
		return ""
	}
	plain, err := g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], nil)
	if err != nil {
		return ""
	}
	return string(plain)
}
