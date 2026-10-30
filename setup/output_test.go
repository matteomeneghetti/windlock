package setup

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// script writes an executable shell script and returns its path.
func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "child")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOutputQuietKeepsChildOutputInTheLog(t *testing.T) {
	root := t.TempDir()
	var term bytes.Buffer
	o := NewOutput(&term, root, "sync", false)
	fmt.Fprintf(o, "app: west update\n")
	if err := runStreaming(o, "", nil, script(t, "echo cloning module; echo more\n")); err != nil {
		t.Fatal(err)
	}
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
	if term.String() != "app: west update\n" {
		t.Fatalf("terminal %q", term.String())
	}
	logs, _ := filepath.Glob(filepath.Join(root, "logs", "sync-*.log"))
	if len(logs) != 1 {
		t.Fatalf("logs %v", logs)
	}
	raw, _ := os.ReadFile(logs[0])
	for _, want := range []string{"app: west update\n", "$ ", "cloning module\nmore\n"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("log lacks %q:\n%s", want, raw)
		}
	}
}

func TestOutputQuietFailureShowsTailAndPath(t *testing.T) {
	root := t.TempDir()
	var term bytes.Buffer
	o := NewOutput(&term, root, "init", false)
	body := ""
	for i := 1; i <= 30; i++ {
		body += fmt.Sprintf("echo line%d\n", i)
	}
	err := runStreaming(o, "", nil, script(t, body+"exit 2\n"))
	_ = o.Close()
	if err == nil {
		t.Fatal("failure not reported")
	}
	msg := err.Error()
	if !strings.Contains(msg, "line30") || !strings.Contains(msg, "line11") || strings.Contains(msg, "line10\n") {
		t.Fatalf("want the last 20 lines:\n%s", msg)
	}
	if !strings.Contains(msg, "full output: "+filepath.Join(root, "logs", "init-")) {
		t.Fatalf("no log path:\n%s", msg)
	}
	if term.Len() != 0 {
		t.Fatalf("child output reached the terminal: %q", term.String())
	}
}

func TestOutputVerboseStreams(t *testing.T) {
	root := t.TempDir()
	var term bytes.Buffer
	o := NewOutput(&term, root, "sync", true)
	err := runStreaming(o, "", nil, script(t, "echo visible; exit 1\n"))
	_ = o.Close()
	if !strings.Contains(term.String(), "visible") {
		t.Fatalf("verbose hid the output: %q", term.String())
	}
	if err == nil || strings.Contains(err.Error(), "full output") {
		t.Fatalf("verbose err %v", err)
	}
}

func TestPruneLogsKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	// Older stamps on one command, newer on another: pruning goes by time.
	for i := 0; i < 12; i++ {
		cmd := "sync"
		if i%2 == 1 {
			cmd = "target-add"
		}
		name := fmt.Sprintf("%s-20261026-1200%02d-%d.log", cmd, i, 4000+i)
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pruneLogs(dir, 10)
	entries, _ := os.ReadDir(dir)
	if len(entries) != 10 {
		t.Fatalf("%d logs left", len(entries))
	}
	for _, gone := range []string{"sync-20261026-120000-4000.log", "target-add-20261026-120001-4001.log"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Fatalf("%s kept", gone)
		}
	}
}
