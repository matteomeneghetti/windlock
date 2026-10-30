package rootlock

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestSecondAcquireWaits(t *testing.T) {
	root := t.TempDir()
	first, err := Acquire(root, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	var log syncBuffer
	got := make(chan *Lock)
	go func() {
		second, err := Acquire(root, &log)
		if err != nil {
			t.Error(err)
		}
		got <- second
	}()
	select {
	case <-got:
		t.Fatal("second run got the lock while the first held it")
	case <-time.After(200 * time.Millisecond):
	}
	if !strings.Contains(log.String(), "waiting for another windlock run (pid") {
		t.Fatalf("log %q", log.String())
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	select {
	case second := <-got:
		if err := second.Release(); err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second run never got the lock")
	}
}
