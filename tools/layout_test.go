package tools

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallPrefixLayouts(t *testing.T) {
	linux := tarGZ(t, map[string]string{
		"cmake-3.31.12-linux-x86_64/bin/cmake":                "linux-cmake",
		"cmake-3.31.12-linux-x86_64/share/cmake-3.31/Modules": "linux-modules",
	})
	mac := tarGZ(t, map[string]string{
		"cmake-3.31.12-macos-universal/CMake.app/Contents/bin/cmake":                "mac-cmake",
		"cmake-3.31.12-macos-universal/CMake.app/Contents/share/cmake-3.31/Modules": "mac-modules",
	})
	python := tarGZ(t, map[string]string{
		"python/bin/python3":       "py",
		"python/lib/libpython.txt": "lib",
	})

	root := t.TempDir()
	host := filepath.Join(root, "host")
	if err := installPrefix(root, host, linux, "linux-sum", "cmake"); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(host, "bin", "cmake"), "linux-cmake")
	assertFile(t, filepath.Join(host, "share", "cmake-3.31", "Modules"), "linux-modules")

	host = filepath.Join(root, "mac")
	if err := installPrefix(root, host, mac, "mac-sum", "cmake"); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(host, "bin", "cmake"), "mac-cmake")
	assertFile(t, filepath.Join(host, "share", "cmake-3.31", "Modules"), "mac-modules")
	if _, err := os.Stat(filepath.Join(host, "CMake.app")); err == nil {
		t.Fatal("left the app bundle wrapper in the prefix")
	}

	host = filepath.Join(root, "py")
	if err := installPrefix(root, host, python, "py-sum", "python3"); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(host, "bin", "python3"), "py")
	assertFile(t, filepath.Join(host, "lib", "libpython.txt"), "lib")
}

func TestExtractNinjaBareBinary(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "ninja-mac.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("ninja")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("ninja-bin")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil || f.Close() != nil {
		t.Fatal("close")
	}
	bin := filepath.Join(dir, "bin")
	if err := extractNinja(archive, bin); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(bin, "ninja"), "ninja-bin")
}

func tarGZ(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		b := []byte(body)
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(b))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil || gz.Close() != nil {
		t.Fatal("close")
	}
	path := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
