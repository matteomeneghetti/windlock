package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// debugServer is a runner's debug server, running in its own process group:
// west starts the real server as a child, and killing west alone leaves it
// holding the probe. Its own group also keeps Ctrl-C in gdb away from it.
type debugServer struct {
	cmd    *exec.Cmd
	output *serverOutput
	done   chan struct{}
	err    error
	once   sync.Once
}

// startDebugServer starts cmd. Readiness comes from its output, not from
// connecting to its port: OpenOCD allows one gdb connection and would refuse
// gdb after a probe, and J-Link's -singlerun exits when the first one closes.
func startDebugServer(cmd *exec.Cmd, marker string, verbose bool, log io.Writer) (*debugServer, error) {
	out := &serverOutput{w: log, marker: marker, verbose: verbose, ready: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = 3 * time.Second
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &debugServer{cmd: cmd, output: out, done: make(chan struct{})}
	go func() {
		s.err = cmd.Wait()
		close(s.done)
	}()
	return s, nil
}

// waitReady waits for the server's ready line, failing early if it exits.
func (s *debugServer) waitReady(limit time.Duration) error {
	select {
	case <-s.output.ready:
		return nil
	case <-s.done:
		return fmt.Errorf("the debug server exited before it was ready: %v; see its output above", s.err)
	case <-time.After(limit):
		return fmt.Errorf("the debug server was not ready within %s; see its output above", limit)
	}
}

// stop ends the whole process group, gently first so the server can release
// the probe.
func (s *debugServer) stop() {
	s.once.Do(func() {
		signalGroup(s.cmd, false)
		select {
		case <-s.done:
			return
		case <-time.After(3 * time.Second):
		}
		signalGroup(s.cmd, true)
		<-s.done
	})
}

// stopOnSignal stops the server if windlock is interrupted, since the server's
// own process group would otherwise outlive it. The returned func cancels this.
func (s *debugServer) stopOnSignal() func() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		if _, ok := <-c; ok {
			s.stop()
			os.Exit(130)
		}
	}()
	return func() {
		signal.Stop(c)
		close(c)
	}
}

// serverOutput shows the server's output until its ready line. After that the
// server would write over gdb's terminal, so only -v keeps showing it.
type serverOutput struct {
	w       io.Writer
	marker  string
	verbose bool
	ready   chan struct{}
	mu      sync.Mutex
	tail    string // kept so a marker split across writes is still seen
	seen    bool
}

func (o *serverOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.seen || o.verbose {
		_, _ = o.w.Write(p)
	}
	if !o.seen {
		text := o.tail + string(p)
		if strings.Contains(text, o.marker) {
			o.seen = true
			close(o.ready)
		} else if keep := len(o.marker); len(text) > keep {
			o.tail = text[len(text)-keep:]
		} else {
			o.tail = text
		}
	}
	return len(p), nil
}
