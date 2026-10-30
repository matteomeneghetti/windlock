package tools

import (
	"reflect"
	"strings"
	"testing"
)

func TestHostPinsMacOS(t *testing.T) {
	linux, err := HostPins("linux-x86_64")
	if err != nil {
		t.Fatal(err)
	}
	for _, triple := range []string{"macos-aarch64", "macos-x86_64"} {
		mac, err := HostPins(triple)
		if err != nil {
			t.Fatal(err)
		}
		// The Mac runs only the probe side, at the versions Linux pins.
		var names []string
		for _, a := range mac {
			names = append(names, a.Name)
			if a.Version != mustTool(t, linux, a.Name).Version {
				t.Fatalf("%s %s %s differs from linux", triple, a.Name, a.Version)
			}
		}
		if !reflect.DeepEqual(names, []string{"python", "uv", "openocd", "dfu-util"}) {
			t.Fatalf("%s pins %v", triple, names)
		}
	}
	arm, _ := HostPins("macos-aarch64")
	intel, _ := HostPins("macos-x86_64")
	if got := mustTool(t, arm, "python").SHA256; got != "a18cc33862b53bd54a148c710def5375c1b47470cccedf817a19c2d211764584" {
		t.Fatalf("aarch64 python sha256 %s", got)
	}
	if got := mustTool(t, intel, "python").SHA256; got != "869e0b30959beccc616ce14907439f4d0b3170c5830a2362dade07f07b0ed3b8" {
		t.Fatalf("x86_64 python sha256 %s", got)
	}
	if mustTool(t, arm, "openocd").File == mustTool(t, intel, "openocd").File {
		t.Fatal("macos openocd pins are not per-triple")
	}
}

func TestNoWindowsPins(t *testing.T) {
	if _, err := HostPins("windows-x86_64"); err == nil {
		t.Fatal("windows has host pins")
	}
	if _, err := HostPins("macos-x86_64 "); err == nil {
		t.Fatal("unknown triple was accepted")
	}
}

func mustTool(t *testing.T, pins []Archive, name string) Archive {
	t.Helper()
	a, ok := findTool(pins, name)
	if !ok {
		t.Fatalf("missing %s", name)
	}
	return a
}

func findTool(pins []Archive, name string) (Archive, bool) {
	for _, a := range pins {
		if a.Name == name {
			return a, true
		}
	}
	return Archive{}, false
}

func sameVersions(a, b []Archive) error {
	if !reflect.DeepEqual(versionsOf(a), versionsOf(b)) {
		return errVersions
	}
	return nil
}

var errVersions = versionsError{}

type versionsError struct{}

func (versionsError) Error() string { return "host-tool versions differ" }

func TestFlashToolsOnlyForFlash(t *testing.T) {
	for _, triple := range []string{"linux-x86_64", "linux-aarch64", "macos-aarch64", "macos-x86_64"} {
		pins, err := HostPins(triple)
		if err != nil {
			t.Fatal(err)
		}
		var flash, install []string
		for _, a := range pins {
			if a.forFlash() {
				flash = append(flash, a.Name)
			}
			if !a.flashOnly() {
				install = append(install, a.Name)
			}
		}
		if !reflect.DeepEqual(flash, []string{"python", "uv", "openocd", "dfu-util"}) {
			t.Fatalf("%s flash set %v", triple, flash)
		}
		wantInstall := []string{"cmake", "ninja", "python", "uv", "dtc", "gperf"}
		if strings.HasPrefix(triple, "macos-") {
			wantInstall = []string{"python", "uv"}
		}
		if !reflect.DeepEqual(install, wantInstall) {
			t.Fatalf("%s install set %v", triple, install)
		}
		if ocd := mustTool(t, pins, "openocd"); !strings.Contains(ocd.File, map[string]string{
			"linux-x86_64": "linux-x64", "linux-aarch64": "linux-arm64", "macos-aarch64": "darwin-arm64", "macos-x86_64": "darwin-x64",
		}[triple]) {
			t.Fatalf("%s openocd %s", triple, ocd.File)
		}
	}
}
