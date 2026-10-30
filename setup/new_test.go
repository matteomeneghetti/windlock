package setup

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// recordingSteps stands in for init and target add and records the calls.
type recordingSteps struct {
	calls     []string
	targetErr error
}

func (r *recordingSteps) steps() newSteps {
	return newSteps{
		init: func(appDir, revision string, log io.Writer) error {
			r.calls = append(r.calls, "init "+filepath.Base(appDir)+" "+revision)
			return nil
		},
		targetAdd: func(appDir, name, board, source string, opts TargetOptions, log io.Writer) error {
			r.calls = append(r.calls, strings.Join([]string{"target", filepath.Base(appDir), name, board, source}, " "))
			// new adds the default target and leaves sysbuild to detection.
			if !opts.Default || opts.Sysbuild != nil {
				r.calls = append(r.calls, "unexpected options")
			}
			return r.targetErr
		},
	}
}

func TestNewWritesSkeleton(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blinky")
	var rec recordingSteps
	var log bytes.Buffer
	if err := rec.steps().run(dir, "", "", "v4.3.1", &log); err != nil {
		t.Fatal(err)
	}
	var got []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			got = append(got, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".gitignore", "apps/blinky/CMakeLists.txt", "apps/blinky/prj.conf", "apps/blinky/src/main.c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files %v, want %v", got, want)
	}
	read := func(rel string) string {
		raw, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	if s := read("apps/blinky/CMakeLists.txt"); !strings.Contains(s, "project(blinky)") || !strings.Contains(s, "find_package(Zephyr REQUIRED HINTS $ENV{ZEPHYR_BASE})") {
		t.Fatalf("CMakeLists.txt:\n%s", s)
	}
	if s := read("apps/blinky/src/main.c"); !strings.Contains(s, `"Hello from blinky on %s\n", CONFIG_BOARD_TARGET`) {
		t.Fatalf("main.c:\n%s", s)
	}
	if read(".gitignore") != "build/\n" {
		t.Fatal(".gitignore")
	}
	if len(rec.calls) != 0 {
		t.Fatalf("without --board it ran %v", rec.calls)
	}
	for _, want := range []string{"windlock init --zephyr v4.3.1", "windlock target add blinky --board <board> --source apps/blinky"} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("next steps lack %q:\n%s", want, log.String())
		}
	}
}

func TestNewWithBoardInitsAndAddsTarget(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj")
	var rec recordingSteps
	if err := rec.steps().run(dir, "app", "qemu_cortex_m3", "v4.4.2", io.Discard); err != nil {
		t.Fatal(err)
	}
	if want := []string{"init proj v4.4.2", "target proj app qemu_cortex_m3 apps/app"}; !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("calls %v, want %v", rec.calls, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "apps", "app", "src", "main.c")); err != nil {
		t.Fatal(err)
	}

	failing := recordingSteps{targetErr: errors.New("board qemu_typo not found")}
	err := failing.steps().run(filepath.Join(t.TempDir(), "p2"), "app", "qemu_typo", "", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "board qemu_typo not found") || !strings.Contains(err.Error(), "windlock target add app") {
		t.Fatalf("target failure err %v", err)
	}
}

func TestNewRefusesWithoutWriting(t *testing.T) {
	var rec recordingSteps
	busy := t.TempDir()
	writeFile(t, filepath.Join(busy, "keep.txt"), "mine")
	if err := rec.steps().run(busy, "app", "", "", io.Discard); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("non-empty dir err %v", err)
	}
	if entries, _ := os.ReadDir(busy); len(entries) != 1 {
		t.Fatalf("wrote into a non-empty dir: %v", entries)
	}
	file := filepath.Join(t.TempDir(), "afile")
	writeFile(t, file, "x")
	if err := rec.steps().run(file, "app", "", "", io.Discard); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file err %v", err)
	}
	bad := filepath.Join(t.TempDir(), "my project")
	if err := rec.steps().run(bad, "", "", "", io.Discard); err == nil || !strings.Contains(err.Error(), "--name") {
		t.Fatalf("bad default name err %v", err)
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal("created the directory for an invalid name")
	}
	if len(rec.calls) != 0 {
		t.Fatalf("ran %v", rec.calls)
	}
	// An existing empty directory is fine.
	empty := t.TempDir()
	if err := rec.steps().run(empty, "app", "", "", io.Discard); err != nil {
		t.Fatal(err)
	}
}
