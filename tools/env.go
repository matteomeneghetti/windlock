package tools

import (
	"os"
	"path/filepath"
)

// RootEnv overrides the toolchain root. When it is empty the root is
// ~/.local/share/windlock.
const RootEnv = "WINDLOCK_ROOT"

// Root returns the toolchain root. A relative WINDLOCK_ROOT is
// made absolute.
func Root() (string, error) {
	root := os.Getenv(RootEnv)
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".local", "share", "windlock")
	}
	return filepath.Abs(root)
}

// BinDir is <root>/host/<id>/bin.
func BinDir(root, id string) string {
	return filepath.Join(root, "host", id, "bin")
}
