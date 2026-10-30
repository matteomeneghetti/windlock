package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureArchiveUsesCache(t *testing.T) {
	root := t.TempDir()
	const sum = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	dest := cachePath(root, sum, "hello.txt")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, cached, err := ensureArchive(root, "https://example.invalid/hello.txt", sum, "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !cached || got != dest {
		t.Fatalf("cached=%v path=%s", cached, got)
	}
}

func TestEnsureArchiveRejectsHTTP(t *testing.T) {
	_, _, err := ensureArchive(t.TempDir(), "http://example.com/a.tar.gz", "abc", "a.tar.gz")
	if err == nil {
		t.Fatal("http URL was accepted")
	}
}

func TestFileSHA256Mismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, err := fileSHA256(path, "0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("mismatched file was accepted")
	}
	ok, err = fileSHA256(filepath.Join(dir, "missing"), "abc")
	if err != nil || ok {
		t.Fatalf("missing file ok=%v err=%v", ok, err)
	}
}
