//go:build !darwin && !linux

package tty

// Elsewhere (Windows) windlock does not ask for a terminal.
func isTerminal(uintptr) bool { return false }
