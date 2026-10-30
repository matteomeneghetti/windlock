package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"windlock/host"
	"windlock/invoke"
	"windlock/lockfile"
)

func TestCLIHost(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"host"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	want, err := host.Current()
	if err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); got != want+"\n" {
		t.Fatalf("host printed %q, want %q", got, want+"\n")
	}
}

func TestCLIRejectsRun(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"run"}, nil, &stdout, &stderr); code == 0 {
		t.Fatal("run was accepted")
	}
	if !strings.Contains(stderr.String(), "unknown command \"run\"") {
		t.Fatalf("stderr %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("run wrote stdout: %s", stdout.String())
	}
}

func TestCLIRemovedCommands(t *testing.T) {
	for _, name := range []string{"env", "tools", "backend"} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{name}, nil, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "unknown command") {
			t.Fatalf("%s: exit %d stderr %q", name, code, stderr.String())
		}
	}
}

func TestCLIRejectsContainer(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"container"}, nil, &stdout, &stderr); code == 0 {
		t.Fatal("container was accepted")
	}
	if !strings.Contains(stderr.String(), "unknown command \"container\"") {
		t.Fatalf("stderr %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("container wrote stdout: %s", stdout.String())
	}
}

func TestCLILockRoundTrip(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "lockfile", "testdata", "windlock.lock.yml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, lockfile.Filename)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"write", path}, bytes.NewReader(raw), &stdout, &stderr); code != 0 {
		t.Fatalf("write exit %d: %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"read", path}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("read exit %d: %s", code, stderr.String())
	}
	got, err := lockfile.Read(&stdout)
	if err != nil {
		t.Fatal(err)
	}
	want, err := lockfile.Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("cli round trip mismatch\nwant %#v\ngot  %#v", want, got)
	}
}

func TestCLIUsageErrors(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"add", "v4.3.1"}, `unknown command "add"`},
		{[]string{"target"}, "usage:"},
		{[]string{"target", "add", "app"}, "usage:"},
		{[]string{"target", "add", "app", "--board", "acme_board"}, "usage:"},
		{[]string{"target", "add", "--board", "acme_board", "--source", "apps/app"}, "usage:"},
		{[]string{"target", "add", "app", "--bord", "x", "--source", "y"}, "flag provided but not defined: -bord"},
		{[]string{"target", "rename", "app"}, `unknown target command "rename"`},
		{[]string{"target", "remove"}, "usage:"},
		{[]string{"zephyr", "upgrade", "v4.4.2"}, `unknown zephyr command "upgrade"`},
		{[]string{"zephyr", "add"}, "usage:"},
		{[]string{"init", "extra"}, "usage:"},
		{[]string{"build", ""}, "usage:"},
		{[]string{"west"}, "usage:"},
		{[]string{"new"}, "usage:"},
		{[]string{"target", "add", "app", "--board", "b", "--source", "s", "--sysbuild", "--no-sysbuild"}, "usage:"},
		{[]string{"new", "--board", "x"}, "usage:"},
		{[]string{"new", "demo", "--bord", "x"}, "flag provided but not defined: -bord"},
	}
	for _, c := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(c.args, nil, &stdout, &stderr); code != 1 {
			t.Fatalf("%v: exit %d", c.args, code)
		}
		if !strings.Contains(stderr.String(), c.want) {
			t.Fatalf("%v: stderr %q lacks %q", c.args, stderr.String(), c.want)
		}
		if stdout.Len() != 0 {
			t.Fatalf("%v wrote stdout: %s", c.args, stdout.String())
		}
	}
}

func TestCLIProjectCommandsNeedInit(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	for _, args := range [][]string{{"target", "list"}, {"zephyr", "list"}, {"target", "default", "app"}, {"zephyr", "remove", "v4.3.1"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, nil, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "windlock init first") {
			t.Fatalf("%v: exit %d stderr %q", args, code, stderr.String())
		}
	}
}

func TestCLIPassesChildExitCode(t *testing.T) {
	commands["test-exit"] = func([]string, streams) error { return &invoke.ExitError{Code: 7} }
	t.Cleanup(func() { delete(commands, "test-exit") })
	var stdout, stderr bytes.Buffer
	if code := run([]string{"test-exit"}, nil, &stdout, &stderr); code != 7 {
		t.Fatalf("exit %d, want 7", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("windlock added output to the child's error: %q", stderr.String())
	}
}

func TestCLIVerboseFlag(t *testing.T) {
	for _, flag := range []string{"-v", "--verbose"} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{flag, "host"}, nil, &stdout, &stderr); code != 0 || stdout.Len() == 0 {
			t.Fatalf("%s host: exit %d stdout %q stderr %q", flag, code, stdout.String(), stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-v"}, nil, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("-v alone: exit %d stderr %q", code, stderr.String())
	}
}

func TestCLIHelp(t *testing.T) {
	for _, arg := range []string{"-h", "--help", "help"} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{arg}, nil, &stdout, &stderr); code != 0 {
			t.Fatalf("%s: exit %d", arg, code)
		}
		if !strings.Contains(stdout.String(), "usage:") || stderr.Len() != 0 {
			t.Fatalf("%s: stdout %q stderr %q", arg, stdout.String(), stderr.String())
		}
	}
}
