//go:build !windows

package main

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestServerOutputFindsSplitMarker(t *testing.T) {
	var log bytes.Buffer
	o := &serverOutput{w: &log, marker: "for gdb connections", ready: make(chan struct{})}
	for _, part := range []string{"Listening on port 3333 for g", "db connections\n", "later noise\n"} {
		if _, err := o.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-o.ready:
	default:
		t.Fatal("marker split across writes not seen")
	}
	if strings.Contains(log.String(), "later noise") {
		t.Fatalf("output after ready was shown: %q", log.String())
	}
}

func TestServerOutputVerboseKeepsShowing(t *testing.T) {
	var log bytes.Buffer
	o := &serverOutput{w: &log, marker: "ready", verbose: true, ready: make(chan struct{})}
	_, _ = o.Write([]byte("ready\nlater noise\n"))
	_, _ = o.Write([]byte("more\n"))
	if !strings.Contains(log.String(), "more") {
		t.Fatalf("verbose output %q", log.String())
	}
}

// west starts the real server as a child, which must not outlive stop.
func TestDebugServerStopEndsChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	script := "sleep 60 & echo $! > '" + pidFile + "'; echo for gdb connections; wait"
	var log bytes.Buffer
	s, err := startDebugServer(exec.Command("/bin/sh", "-c", script), "for gdb connections", false, &log)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	if err := s.waitReady(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(child, 0); err != nil {
		t.Fatalf("child not running: %v", err)
	}
	s.stop()
	s.stop() // safe to call twice, as the deferred stop does
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(child, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("child %d outlived stop", child)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestDebugServerExitBeforeReady(t *testing.T) {
	var log bytes.Buffer
	s, err := startDebugServer(exec.Command("/bin/sh", "-c", "echo No J-Link device found; exit 1"), "for gdb connections", false, &log)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	if err := s.waitReady(10 * time.Second); err == nil || !strings.Contains(err.Error(), "exited before") {
		t.Fatalf("err %v", err)
	}
	if !strings.Contains(log.String(), "No J-Link") {
		t.Fatalf("server output hidden: %q", log.String())
	}
}

func TestDebugServerNotReadyInTime(t *testing.T) {
	var log bytes.Buffer
	s, err := startDebugServer(exec.Command("/bin/sh", "-c", "sleep 30"), "never", false, &log)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	if err := s.waitReady(100 * time.Millisecond); err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("err %v", err)
	}
}

// The bug this design fixes: waiting must not use up the server's one connection.
func TestWaitReadyMakesNoConnection(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan struct{}, 1)
	go func() {
		if c, err := l.Accept(); err == nil {
			c.Close()
			accepted <- struct{}{}
		}
	}()
	var log bytes.Buffer
	s, err := startDebugServer(exec.Command("/bin/sh", "-c", "echo for gdb connections; sleep 30"), "for gdb connections", false, &log)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	if err := s.waitReady(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-accepted:
		t.Fatal("waiting connected to the server")
	case <-time.After(100 * time.Millisecond):
	}
}
