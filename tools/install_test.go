package tools

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cachedNinja puts a ninja zip holding body in root's download cache and
// returns its pin.
func cachedNinja(t *testing.T, root, body string) Archive {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("ninja")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	a := Archive{Name: "ninja", Version: "1", URL: "https://example.invalid/ninja.zip", SHA256: hex.EncodeToString(sum[:]), File: "ninja.zip", Kind: kindNinja}
	path := cachePath(root, a.SHA256, a.File)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestInstallOneRedoesUnfinishedInstall(t *testing.T) {
	root := t.TempDir()
	hostDir := filepath.Join(root, "host", "id")
	bin := filepath.Join(hostDir, "bin")
	a := cachedNinja(t, root, "good")

	// A binary left by an interrupted install, with no marker.
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "ninja"), []byte("half"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installOne(root, hostDir, bin, a, io.Discard); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(bin, "ninja"), "good")
	assertFile(t, filepath.Join(hostDir, ".installed", "ninja"), a.SHA256+"\n")

	// A finished install is kept as it is.
	if err := os.WriteFile(filepath.Join(bin, "ninja"), []byte("kept"), 0o755); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	if err := installOne(root, hostDir, bin, a, &log); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(bin, "ninja"), "kept")
	if !strings.Contains(log.String(), "already installed") {
		t.Fatalf("log %q", log.String())
	}

	// A marker for another archive is not this one.
	b := cachedNinja(t, root, "newer")
	if err := installOne(root, hostDir, bin, b, io.Discard); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(bin, "ninja"), "newer")
}

func TestInstalledNeedsMarkerAndBinary(t *testing.T) {
	dir := t.TempDir()
	marker, dest := filepath.Join(dir, "marker"), filepath.Join(dir, "tool")
	if installed(marker, dest, "abc") {
		t.Fatal("nothing on disk counted as installed")
	}
	if err := os.WriteFile(marker, []byte("abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if installed(marker, dest, "abc") {
		t.Fatal("a marker without its binary counted as installed")
	}
	if err := os.WriteFile(dest, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if !installed(marker, dest, "abc") || installed(marker, dest, "abd") {
		t.Fatal("marker check")
	}
}
