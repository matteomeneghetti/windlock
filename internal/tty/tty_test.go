package tty

import (
	"os"
	"testing"
)

func TestDevNullIsNotATerminal(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if Is(f) {
		t.Fatal("/dev/null counted as a terminal")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if Is(r) || Is(w) {
		t.Fatal("a pipe counted as a terminal")
	}
}
