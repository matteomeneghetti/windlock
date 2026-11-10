package tools

import (
	"reflect"
	"strings"
	"testing"
)

var allTriples = []string{"linux-x86_64", "linux-aarch64", "macos-aarch64", "macos-x86_64"}

func TestDefaultsSplitBuildAndFlash(t *testing.T) {
	for _, triple := range allTriples {
		flash, err := DefaultFlashTools(triple)
		if err != nil {
			t.Fatal(err)
		}
		if got := names(flash); !reflect.DeepEqual(got, []string{"python", "uv", "openocd", "dfu-util"}) {
			t.Fatalf("%s flash tools %v", triple, got)
		}
		ocd := mustTool(t, flash, "openocd")
		if !strings.Contains(ocd.URL, map[string]string{
			"linux-x86_64": "linux-x64", "linux-aarch64": "linux-arm64", "macos-aarch64": "darwin-arm64", "macos-x86_64": "darwin-x64",
		}[triple]) {
			t.Fatalf("%s openocd %s", triple, ocd.URL)
		}
		build, err := DefaultBuildTools(triple)
		if strings.HasPrefix(triple, "macos-") {
			if err == nil {
				t.Fatalf("%s has build tools %v", triple, names(build))
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := names(build); !reflect.DeepEqual(got, []string{"cmake", "ninja", "python", "uv", "dtc", "gperf"}) {
			t.Fatalf("%s build tools %v", triple, got)
		}
		// A Linux host's probe side uses the container's Python and uv.
		for _, name := range []string{"python", "uv"} {
			if mustTool(t, build, name) != mustTool(t, flash, name) {
				t.Fatalf("%s %s differs between build and flash", triple, name)
			}
		}
	}
	if _, err := DefaultFlashTools("windows-x86_64"); err == nil {
		t.Fatal("windows has flash tools")
	}
}

func TestDefaultsValidate(t *testing.T) {
	for _, triple := range allTriples {
		flash, _ := DefaultFlashTools(triple)
		if err := Validate(Set{Tools: flash, Sources: DefaultSources()}); err != nil {
			t.Fatalf("%s flash: %v", triple, err)
		}
		if build, err := DefaultBuildTools(triple); err == nil {
			if err := Validate(Set{Tools: build, Sources: DefaultSources()}); err != nil {
				t.Fatalf("%s build: %v", triple, err)
			}
		}
	}
	arm, _ := DefaultFlashTools("macos-aarch64")
	if got := mustTool(t, arm, "python").SHA256; got != "a18cc33862b53bd54a148c710def5375c1b47470cccedf817a19c2d211764584" {
		t.Fatalf("aarch64 python sha256 %s", got)
	}
}

func names(ts []Tool) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Name)
	}
	return out
}

func mustTool(t *testing.T, ts []Tool, name string) Tool {
	t.Helper()
	tool, ok := findTool(ts, name)
	if !ok {
		t.Fatalf("missing %s", name)
	}
	return tool
}
