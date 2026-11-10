package setup

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/matteomeneghetti/windlock/lockfile"
	"github.com/matteomeneghetti/windlock/tools"
)

var update = flag.Bool("update", false, "rewrite testdata golden files")

// Rows from the SDK 1.0.1 sha256.sum release asset. It has no macos-x86_64.
const sdk101Sum = `
b9805b691f2f0a8926c92694cae378d05ba07b76abca745e216fcc52753cc4d6  toolchain_gnu_linux-aarch64_arm-zephyr-eabi.tar.xz
21b85981cb5a1818d9bc53d82af80f208946ec038b982ff1907287572ed3a634  toolchain_gnu_linux-x86_64_arm-zephyr-eabi.tar.xz
d79c5bfc68e679488659bea289a4026e52a64f03338875c8c9c850fff13cee30  zephyr-sdk-1.0.1_linux-aarch64_minimal.tar.xz
ca9bc0ff66fafca1dac9d592a36d953cf16d096a9d09b1c0357f021cf9f6a7eb  zephyr-sdk-1.0.1_linux-x86_64_minimal.tar.xz
867063901f39528a6175a80ebc20367bd6cb440593e7e2650eda30392f1f6b65  zephyr-sdk-1.0.1_macos-aarch64_minimal.tar.xz
17a557e7c6cea5e5c589508fe45a3f26aca4fa2c6153fe3e7b03aeb4b93fea4b  zephyr-sdk-1.0.1_windows-x86_64_minimal.7z
`

// Rows in the shape of the SDK 0.17.4 sha256.sum, which has every host.
const sdk0174Sum = `
1111111111111111111111111111111111111111111111111111111111111111  zephyr-sdk-0.17.4_macos-x86_64_minimal.tar.xz
2222222222222222222222222222222222222222222222222222222222222222  zephyr-sdk-0.17.4_macos-aarch64_minimal.tar.xz
3333333333333333333333333333333333333333333333333333333333333333  zephyr-sdk-0.17.4_linux-x86_64_minimal.tar.xz
4444444444444444444444444444444444444444444444444444444444444444  zephyr-sdk-0.17.4_linux-aarch64_minimal.tar.xz
5555555555555555555555555555555555555555555555555555555555555555  zephyr-sdk-0.17.4_windows-x86_64_minimal.7z
`

var testSums = map[string]string{"1.0.1": sdk101Sum, "0.17.4": sdk0174Sum}

func testEntry(name, revision, commit, sdk string) lockfile.Zephyr {
	return lockfile.Zephyr{
		Name:           name,
		URL:            zephyrURL,
		Revision:       revision,
		Commit:         commit,
		SDK:            sdk,
		Toolchains:     []string{"arm-zephyr-eabi"},
		ManifestSHA256: strings.Repeat("ab", 32),
		Projects: []lockfile.Project{
			{Name: "cmsis_6", Path: "modules/hal/cmsis_6", Revision: strings.Repeat("c", 40)},
		},
		PythonInputsSHA256: strings.Repeat("cd", 32),
		PythonRequirements: []string{"pyelftools==0.32", "west==1.5.0", "windows-curses==2.4.1 ; sys_platform == 'win32'"},
	}
}

func testEntries() []lockfile.Zephyr {
	return []lockfile.Zephyr{
		testEntry("4.4.2", "v4.4.2", "dccb09599635bdff17633fa7e9dab014b91dce90", "1.0.1"),
		testEntry("4.3.1", "v4.3.1", "75f67d766726351b30199f9a2bf55803d717a3be", "0.17.4"),
	}
}

func mustLock(t *testing.T, current string) lockfile.Lockfile {
	t.Helper()
	lock, err := assembleLock(testEntries(), "1.5.0", testSums, mustPins(), current)
	if err != nil {
		t.Fatal(err)
	}
	return lock
}

func encodeLock(t *testing.T, lock lockfile.Lockfile) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := lockfile.Write(&buf, lock); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The lockfile is the same bytes whichever host writes it, and those bytes
// are testdata/windlock.lock.yml.
func TestAssembleLockIsIdenticalOnEveryHost(t *testing.T) {
	want := encodeLock(t, mustLock(t, "linux-x86_64"))
	for _, triple := range []string{"linux-aarch64"} {
		if got := encodeLock(t, mustLock(t, triple)); !bytes.Equal(got, want) {
			t.Fatalf("lockfile written on %s differs:\n%s\n---\n%s", triple, got, want)
		}
	}
	golden := filepath.Join("testdata", "windlock.lock.yml")
	if *update {
		if err := os.WriteFile(golden, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, want) {
		t.Fatalf("%s is stale; rerun go test ./setup -run IsIdentical -update\n%s", golden, want)
	}
	back, err := lockfile.Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := lockfile.Diff(mustLock(t, "linux-x86_64"), back); d != nil {
		t.Fatalf("golden round trip: %v", d)
	}
}

func mustPins() pinSection {
	ps, err := defaultPins()
	if err != nil {
		panic(err)
	}
	return ps
}

func TestAssembleLockPlatforms(t *testing.T) {
	lock := mustLock(t, "linux-x86_64")
	if lock.Schema != lockfile.Schema || lock.West != "1.5.0" {
		t.Fatalf("schema %d west %s", lock.Schema, lock.West)
	}
	if got := sortedKeys(lock.Sources); !reflect.DeepEqual(got, []string{"bison", "flex", "libusb", "m4"}) {
		t.Fatalf("sources %v", got)
	}
	if got := sortedKeys(lock.Platforms); !reflect.DeepEqual(got, []string{"linux-aarch64", "linux-x86_64", "macos-aarch64", "macos-x86_64"}) {
		t.Fatalf("platforms %v", got)
	}
	flash := []string{"dfu-util", "openocd", "python", "uv"}
	// The Mac records only its probe side.
	for _, triple := range []string{"macos-aarch64", "macos-x86_64"} {
		mac := lock.Platforms[triple]
		if len(mac.SDK) != 0 || len(mac.BuildTools) != 0 || !reflect.DeepEqual(sortedKeys(mac.FlashTools), flash) {
			t.Fatalf("%s %+v", triple, mac)
		}
	}
	for _, triple := range []string{"linux-x86_64", "linux-aarch64"} {
		p := lock.Platforms[triple]
		if !reflect.DeepEqual(sortedKeys(p.BuildTools), []string{"cmake", "dtc", "gperf", "ninja", "python", "uv"}) ||
			!reflect.DeepEqual(sortedKeys(p.FlashTools), flash) || len(p.SDK) != 2 {
			t.Fatalf("%s %+v", triple, p)
		}
		if p.BuildTools["cmake"].Version != "3.31.12" {
			t.Fatalf("%s cmake %+v", triple, p.BuildTools["cmake"])
		}
	}
	if b := lock.Platforms["linux-aarch64"].SDK["1.0.1"]; b.Filename != "zephyr-sdk-1.0.1_linux-aarch64_minimal.tar.xz" {
		t.Fatalf("linux-aarch64 bundle %+v", b)
	}
}

// otherNinja is a real Ninja release the defaults don't pin.
var otherNinja = lockfile.Tool{
	Version: "1.13.1",
	URL:     "https://github.com/ninja-build/ninja/releases/download/v1.13.1/ninja-linux.zip",
	SHA256:  strings.Repeat("ab", 32),
}

func TestLockedPinsSurviveRewrites(t *testing.T) {
	lock := mustLock(t, "linux-x86_64")
	lock.Platforms["linux-x86_64"].BuildTools["ninja"] = otherNinja
	// A command other than init rewrites the lockfile with its pins.
	again, err := assembleLock(lock.Zephyr, lock.West, testSums, lockedPins(lock), "linux-x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Platforms["linux-x86_64"].BuildTools["ninja"]; got != otherNinja {
		t.Fatalf("ninja %+v", got)
	}
	if d, _ := lockfile.Diff(lock, again); d != nil {
		t.Fatalf("rewrite changed %v", d)
	}
	// init locks this windlock's pins instead.
	reset, err := assembleLock(lock.Zephyr, lock.West, testSums, mustPins(), "linux-x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if got := reset.Platforms["linux-x86_64"].BuildTools["ninja"]; got.Version != "1.13.2" {
		t.Fatalf("init ninja %+v", got)
	}
}

func TestBuildAndFlashSets(t *testing.T) {
	lock := mustLock(t, "linux-x86_64")
	build, err := BuildSet(lock, "linux-x86_64")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range build.Tools {
		names = append(names, tool.Name)
	}
	if !reflect.DeepEqual(names, []string{"cmake", "dtc", "gperf", "ninja", "python", "uv"}) || len(build.Sources) != 4 {
		t.Fatalf("build set %v sources %d", names, len(build.Sources))
	}
	if err := tools.Validate(build); err != nil {
		t.Fatal(err)
	}
	if set, err := FlashSet(lock, "macos-aarch64"); err != nil || len(set.Tools) != 4 {
		t.Fatalf("mac flash set %+v err %v", set, err)
	}
	if _, err := BuildSet(lock, "macos-aarch64"); err == nil || !strings.Contains(err.Error(), "no build tools for macos-aarch64") {
		t.Fatalf("mac build set err %v", err)
	}
	if _, err := FlashSet(lock, "solaris-sparc"); err == nil {
		t.Fatal("unknown platform has a flash set")
	}

	// The build dir follows the lockfile's pins, not this windlock's.
	root := t.TempDir()
	bin, err := HostToolsBin(lock, root, "linux-x86_64")
	if err != nil || bin != tools.BinDirFor(root, build) {
		t.Fatalf("bin %s err %v", bin, err)
	}
	lock.Platforms["linux-x86_64"].BuildTools["ninja"] = otherNinja
	if other, _ := HostToolsBin(lock, root, "linux-x86_64"); other == bin {
		t.Fatal("another ninja kept the same host-tools dir")
	}
}

func TestAssembleLockRejectsUninstallableTools(t *testing.T) {
	pins := mustPins()
	pins.build["linux-x86_64"] = map[string]lockfile.Tool{"make": otherNinja}
	if _, err := assembleLock(testEntries(), "1.5.0", testSums, pins, "linux-x86_64"); err == nil || !strings.Contains(err.Error(), "no install recipe for tool make") {
		t.Fatalf("unknown tool err %v", err)
	}
	pins = mustPins()
	delete(pins.sources, "libusb")
	if _, err := assembleLock(testEntries(), "1.5.0", testSums, pins, "linux-x86_64"); err == nil || !strings.Contains(err.Error(), "dfu-util builds with source libusb") {
		t.Fatalf("missing source err %v", err)
	}
}

func TestAssembleLockRejectsUnusableHost(t *testing.T) {
	// Only the container writes lockfiles; a Mac has no SDK bundle to build with.
	_, err := assembleLock(testEntries(), "1.5.0", testSums, mustPins(), "macos-aarch64")
	if err == nil || !strings.Contains(err.Error(), "no build tools for macos-aarch64") {
		t.Fatalf("mac err %v", err)
	}
	// A Linux host with build tools but no bundle names the hosts that have one.
	lock := mustLock(t, "linux-x86_64")
	p := lock.Platforms["linux-x86_64"]
	p.SDK = nil
	lock.Platforms["linux-x86_64"] = p
	if err := usablePlatform(lock, "linux-x86_64"); err == nil || !strings.Contains(err.Error(), "SDK 1.0.1 bundle for host linux-x86_64") ||
		!strings.Contains(err.Error(), "(recorded hosts: linux-aarch64)") {
		t.Fatalf("missing bundle err %v", err)
	}
	if _, err := assembleLock(testEntries(), "1.5.0", testSums, mustPins(), "linux-aarch64"); err != nil {
		t.Fatalf("linux-aarch64: %v", err)
	}
	bare := lockfile.Lockfile{Platforms: map[string]lockfile.Platform{"linux-aarch64": {}}}
	if err := usablePlatform(bare, "linux-aarch64"); err == nil || !strings.Contains(err.Error(), "no build tools for linux-aarch64") {
		t.Fatalf("no build tools err %v", err)
	}
}

func TestAssembleLockRejectsBadEntries(t *testing.T) {
	cases := map[string]func(*lockfile.Zephyr){
		"commit":   func(e *lockfile.Zephyr) { e.Commit = "abc" },
		"manifest": func(e *lockfile.Zephyr) { e.ManifestSHA256 = "" },
		"Python":   func(e *lockfile.Zephyr) { e.PythonRequirements = nil },
		"sdk":      func(e *lockfile.Zephyr) { e.SDK = "" },
		"url":      func(e *lockfile.Zephyr) { e.URL = "" },
	}
	for want, edit := range cases {
		entries := testEntries()
		edit(&entries[0])
		if _, err := assembleLock(entries, "1.5.0", testSums, mustPins(), "linux-x86_64"); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err %v", want, err)
		}
	}
	twice := append(testEntries(), testEntries()[0])
	if _, err := assembleLock(twice, "1.5.0", testSums, mustPins(), "linux-x86_64"); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("duplicate err %v", err)
	}
	if _, err := assembleLock(testEntries(), "", testSums, mustPins(), "linux-x86_64"); err == nil || !strings.Contains(err.Error(), "west") {
		t.Fatalf("west err %v", err)
	}
	if _, err := assembleLock(testEntries(), "1.5.0", map[string]string{"1.0.1": sdk101Sum}, mustPins(), "linux-x86_64"); err == nil || !strings.Contains(err.Error(), "0.17.4") {
		t.Fatalf("missing sum err %v", err)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
