// Package linuxbin provides the Linux windlock that runs inside the container.
package linuxbin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

// ErrMissing means this build carries no Linux windlock for the container.
var ErrMissing = errors.New("this windlock was built without the Linux windlock the container runs; build it with scripts/build.sh")

// Path returns a Linux windlock for the host's CPU: this executable on
// Linux, else the embedded one, written once under dir by content hash.
func Path(dir string) (string, error) {
	if self, ok := selfIsLinux(); ok {
		return self, nil
	}
	raw, err := embedded()
	if err != nil {
		return "", err
	}
	return writeOnce(dir, raw)
}

// writeOnce stores raw as an executable named by its hash, once.
func writeOnce(dir string, raw []byte) (string, error) {
	sum := sha256.Sum256(raw)
	path := filepath.Join(dir, "windlock-"+hex.EncodeToString(sum[:8]))
	if info, err := os.Stat(path); err == nil && info.Size() == int64(len(raw)) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".windlock-*")
	if err != nil {
		return "", err
	}
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(tmp.Name(), 0o755)); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return path, os.Rename(tmp.Name(), path)
}
