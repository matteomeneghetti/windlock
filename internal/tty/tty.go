// Package tty reports whether a file is a terminal.
package tty

import "os"

// Is reports whether f is a terminal. /dev/null is a character device too,
// so the mode bits alone cannot tell.
func Is(f *os.File) bool { return isTerminal(f.Fd()) }
