// Package rootlock serializes windlock runs that write the toolchain root.
package rootlock

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Lock is a held lock on <root>/.lock; the kernel drops it if the process dies.
type Lock struct{ f *os.File }

// Acquire takes the root's lock, telling log once if another run holds it,
// then waits for it.
func Acquire(root string, log io.Writer) (*Lock, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(root, ".lock")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	held, err := tryLock(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	if !held {
		fmt.Fprintf(log, "waiting for another windlock run%s to finish with %s\n", holder(path), root)
		if err := waitLock(f); err != nil {
			f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
	}
	if err := f.Truncate(0); err == nil {
		_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	}
	return &Lock{f: f}, nil
}

// Release drops the lock.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := unlock(l.f)
	if cerr := l.f.Close(); err == nil {
		err = cerr
	}
	l.f = nil
	return err
}

func holder(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if pid := strings.TrimSpace(string(raw)); pid != "" {
		return " (pid " + pid + ")"
	}
	return ""
}
