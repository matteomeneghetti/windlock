package invoke

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveStaleBuild(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "build", "app")
	cache := filepath.Join(dir, "CMakeCache.txt")

	// No build yet: nothing to do, and nothing to mark.
	if err := removeStaleBuild(dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := recordContainer(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, containerMarker)); err == nil {
		t.Fatal("marked a directory the build never made")
	}
	// A directory with no CMake cache is not a build: left alone.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := removeStaleBuild(dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("removed a directory with no CMake cache")
	}
	// After the container builds, it is left alone.
	if err := os.WriteFile(cache, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := recordContainer(dir); err != nil {
		t.Fatal(err)
	}
	if err := removeStaleBuild(dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatal("removed a build the container made")
	}
	// A CMake cache with no marker came from elsewhere: removed.
	if err := os.Remove(filepath.Join(dir, containerMarker)); err != nil {
		t.Fatal(err)
	}
	if err := removeStaleBuild(dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("stale build still there: %v", err)
	}
}
