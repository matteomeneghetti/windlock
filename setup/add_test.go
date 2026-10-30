package setup

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddRefusesManifestProject(t *testing.T) {
	app := t.TempDir()
	if err := os.WriteFile(filepath.Join(app, "west.yml"), []byte("manifest:\n  self:\n    path: my-firmware\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ZephyrAdd(app, "v4.3.1", io.Discard)
	if err == nil {
		t.Fatal("add ran in a west.yml project")
	}
	for _, want := range []string{"west.yml", "v4.3.1", "windlock init"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q lacks %q", err, want)
		}
	}
}

func TestAddNeedsInit(t *testing.T) {
	err := ZephyrAdd(t.TempDir(), "v4.3.1", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "windlock init") {
		t.Fatalf("err %v", err)
	}
}
