package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// entry is one tar member: a file with body, a directory (name ends in /),
// or a symlink when link is set.
type entry struct{ name, body, link string }

func tarEntries(t *testing.T, entries []entry) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o644}
		switch {
		case e.link != "":
			hdr.Typeflag, hdr.Linkname = tar.TypeSymlink, e.link
		case strings.HasSuffix(e.name, "/"):
			hdr.Typeflag, hdr.Mode = tar.TypeDir, 0o755
		default:
			hdr.Typeflag, hdr.Size = tar.TypeReg, int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil || gz.Close() != nil {
		t.Fatal("close")
	}
	archive := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(archive, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return archive
}

func TestExtractRefusesWritingThroughSymlinks(t *testing.T) {
	cases := map[string][]entry{
		"file through link": {{name: "top/sub/"}, {name: "top/l", link: "sub"}, {name: "top/l/f", body: "x"}},
		"dir through link":  {{name: "top/sub/"}, {name: "top/l", link: "sub"}, {name: "top/l/d/"}},
		"link through link": {{name: "top/sub/"}, {name: "top/l", link: "sub"}, {name: "top/l/k", link: "f"}},
	}
	for name, entries := range cases {
		dest := filepath.Join(t.TempDir(), "out")
		err := extractTar(tarEntries(t, entries), dest, 1)
		if err == nil || !strings.Contains(err.Error(), "goes through the symlink l") {
			t.Fatalf("%s: err %v", name, err)
		}
	}
}

func TestExtractRefusesLinkChainThatEscapes(t *testing.T) {
	// Each link stays inside by name, and x/a is written while x/s is
	// missing; once x/s points at ../d, x/a resolves above the root.
	dir := t.TempDir()
	dest := filepath.Join(dir, "out", "stage")
	archive := tarEntries(t, []entry{
		{name: "top/d/"},
		{name: "top/x/a", link: "s/../.."},
		{name: "top/x/s", link: "../d"},
	})
	if err := extractTar(archive, dest, 1); err == nil || !strings.Contains(err.Error(), "symlink x/a: resolves outside") {
		t.Fatalf("err %v", err)
	}
}

func TestExtractKeepsLinksBetweenLinks(t *testing.T) {
	// The CPython layout: python -> python3 -> python3.12, and a link into bin.
	dest := filepath.Join(t.TempDir(), "out")
	archive := tarEntries(t, []entry{
		{name: "python/bin/python3.12", body: "py"},
		{name: "python/bin/python3", link: "python3.12"},
		{name: "python/bin/python", link: "python3"},
		{name: "python/lib/py", link: "../bin/python"},
		{name: "python/lib/dangling", link: "missing/file"},
	})
	if err := extractTar(archive, dest, 1); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "lib", "py"))
	if err != nil || string(got) != "py" {
		t.Fatalf("lib/py %q err %v", got, err)
	}
}

func TestResolveInside(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "a", "b"), 0o755))
	must(os.Symlink("../..", filepath.Join(root, "a", "b", "up")))
	must(os.Symlink("b", filepath.Join(root, "a", "down")))
	must(os.Symlink("loop", filepath.Join(root, "loop")))
	must(os.Symlink("/etc", filepath.Join(root, "abs")))
	for rel, ok := range map[string]bool{
		"a/b/up":           true,  // back to root
		"a/b/up/a/down":    true,  // root/a/b
		"a/down/up/..":     false, // root/a/b/../.. = root, then ..
		"a/b/up/a/b/up/..": false,
		"loop":             false,
		"abs":              false,
	} {
		if err := resolveInside(root, rel); (err == nil) != ok {
			t.Fatalf("%s: err %v, want ok=%v", rel, err, ok)
		}
	}
}

func TestMergeTreeRefusesLinkOutsideSource(t *testing.T) {
	stage := t.TempDir()
	src := filepath.Join(stage, "prefix")
	if err := os.MkdirAll(filepath.Join(src, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Inside the stage, but outside the prefix that gets merged.
	if err := os.Symlink("../../other", filepath.Join(src, "bin", "x")); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "host")
	if err := mergeTree(src, dst); err == nil || !strings.Contains(err.Error(), "symlink bin/x") {
		t.Fatalf("err %v", err)
	}
}

func TestExtractBzip2(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "top/configure", Mode: 0o755, Size: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	// Go has no bzip2 writer; the system bzip2 makes the fixture.
	dir := t.TempDir()
	plain := filepath.Join(dir, "a.tar")
	if err := os.WriteFile(plain, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bzip2", plain).CombinedOutput(); err != nil {
		t.Skipf("bzip2: %v %s", err, out)
	}
	dest := filepath.Join(dir, "out")
	if err := extractTar(plain+".bz2", dest, 1); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(dest, "configure"), "ok")
}
