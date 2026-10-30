package main

import (
	"os"
	"testing"
)

// The CLI tests exercise the commands as the container's windlock runs
// them; the routing tests clear this to test the host side.
func TestMain(m *testing.M) {
	os.Setenv("WINDLOCK_IN_CONTAINER", "1")
	os.Exit(m.Run())
}
