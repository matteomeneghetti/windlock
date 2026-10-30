package linuxbin

import (
	"errors"
	"os"
	"runtime"
	"testing"
)

func TestWriteOnceIsContentAddressed(t *testing.T) {
	dir := t.TempDir()
	a, err := writeOnce(dir, []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := writeOnce(dir, []byte("one"))
	if err != nil || again != a {
		t.Fatalf("second write %s err %v, want %s", again, err, a)
	}
	b, err := writeOnce(dir, []byte("two"))
	if err != nil || b == a {
		t.Fatalf("new content reused %s err %v", b, err)
	}
	info, err := os.Stat(a)
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("%s mode %v err %v", a, info.Mode(), err)
	}
}

func TestPathWithoutEmbeddedBinary(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux runs its own executable")
	}
	if _, err := embedded(); err == nil {
		t.Skip("this build embeds a Linux windlock")
	}
	if _, err := Path(t.TempDir()); !errors.Is(err, ErrMissing) {
		t.Fatalf("err %v", err)
	}
}
