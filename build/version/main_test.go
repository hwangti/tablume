package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Every file that carries the version agrees with VERSION.
func TestInStep(t *testing.T) {
	if err := Check("../.."); err != nil {
		t.Fatal(err)
	}
}

// Set changes exactly the version spots and Check then passes.
func TestSet(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"VERSION"} {
		copyFile(t, f, root)
	}
	for _, s := range spots {
		copyFile(t, s.file, root)
	}
	if err := Set(root, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	if err := Check(root); err != nil {
		t.Fatal(err)
	}
	if v, _ := Read(root); v != "1.2.3" {
		t.Fatalf("VERSION %q", v)
	}
	if err := Set(root, "1.2"); err == nil {
		t.Fatal("1.2 accepted")
	}
}

func copyFile(t *testing.T, f, root string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("../..", f))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, f)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
