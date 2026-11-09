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
	lock, err := assembleLock(testEntries(), "1.5.0", testSums, current)
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

func TestAssembleLockPlatforms(t *testing.T) {
	lock := mustLock(t, "linux-x86_64")
	if lock.Schema != lockfile.Schema || lock.West != "1.5.0" {
		t.Fatalf("schema %d west %s", lock.Schema, lock.West)
	}
	want := map[string]string{"cmake": "3.31.12", "ninja": "1.13.2", "python": "3.12.15", "dtc": "1.8.1", "gperf": "3.3", "uv": "0.13.6", "openocd": "0.12.0-7", "dfu-util": "0.11"}
	if !reflect.DeepEqual(lock.HostTools, want) {
		t.Fatalf("host tools %v", lock.HostTools)
	}
	if got := sortedKeys(lock.Platforms); !reflect.DeepEqual(got, []string{"linux-aarch64", "linux-x86_64", "macos-aarch64", "macos-x86_64"}) {
		t.Fatalf("platforms %v", got)
	}
	// The Mac records only its probe side, at the same versions.
	for _, triple := range []string{"macos-aarch64", "macos-x86_64"} {
		mac := lock.Platforms[triple]
		if len(mac.SDK) != 0 || !reflect.DeepEqual(sortedKeys(mac.HostTools), []string{"dfu-util", "openocd", "python", "uv"}) {
			t.Fatalf("%s %+v", triple, mac)
		}
	}
	for _, triple := range []string{"linux-x86_64", "linux-aarch64"} {
		if p := lock.Platforms[triple]; len(p.HostTools) != 8 || len(p.SDK) != 2 {
			t.Fatalf("%s %+v", triple, p)
		}
	}
	if b := lock.Platforms["linux-aarch64"].SDK["1.0.1"]; b.Filename != "zephyr-sdk-1.0.1_linux-aarch64_minimal.tar.xz" {
		t.Fatalf("linux-aarch64 bundle %+v", b)
	}
}

func TestAssembleLockRejectsUnusableHost(t *testing.T) {
	// Only the container writes lockfiles; a Mac has no SDK bundle to build with.
	_, err := assembleLock(testEntries(), "1.5.0", testSums, "macos-aarch64")
	if err == nil || !strings.Contains(err.Error(), "SDK 1.0.1 bundle for host macos-aarch64") ||
		!strings.Contains(err.Error(), "linux-aarch64, linux-x86_64") {
		t.Fatalf("missing bundle err %v", err)
	}
	if _, err := assembleLock(testEntries(), "1.5.0", testSums, "linux-aarch64"); err != nil {
		t.Fatalf("linux-aarch64: %v", err)
	}
	bare := lockfile.Lockfile{Platforms: map[string]lockfile.Platform{"linux-aarch64": {}}}
	if err := usablePlatform(bare, "linux-aarch64"); err == nil || !strings.Contains(err.Error(), "no host tools for linux-aarch64") {
		t.Fatalf("no host-tool pins err %v", err)
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
		if _, err := assembleLock(entries, "1.5.0", testSums, "linux-x86_64"); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err %v", want, err)
		}
	}
	twice := append(testEntries(), testEntries()[0])
	if _, err := assembleLock(twice, "1.5.0", testSums, "linux-x86_64"); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("duplicate err %v", err)
	}
	if _, err := assembleLock(testEntries(), "", testSums, "linux-x86_64"); err == nil || !strings.Contains(err.Error(), "west") {
		t.Fatalf("west err %v", err)
	}
	if _, err := assembleLock(testEntries(), "1.5.0", map[string]string{"1.0.1": sdk101Sum}, "linux-x86_64"); err == nil || !strings.Contains(err.Error(), "0.17.4") {
		t.Fatalf("missing sum err %v", err)
	}
}

func TestCheckHostTools(t *testing.T) {
	lock := mustLock(t, "linux-x86_64")
	for _, triple := range tools.PinnedTriples() {
		pins, err := tools.HostPins(triple)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkHostTools(lock, triple, pins); err != nil {
			t.Fatalf("lockfile written on linux, sync on %s: %v", triple, err)
		}
	}
	linux, err := tools.HostPins("linux-x86_64")
	if err != nil {
		t.Fatal(err)
	}
	bad := mustLock(t, "linux-x86_64")
	a := bad.Platforms["linux-x86_64"].HostTools["cmake"]
	a.SHA256 = strings.Repeat("00", 32)
	bad.Platforms["linux-x86_64"].HostTools["cmake"] = a
	if err := checkHostTools(bad, "linux-x86_64", linux); err == nil || !strings.Contains(err.Error(), "cmake") {
		t.Fatalf("cmake sha mismatch err %v", err)
	}
	old := mustLock(t, "linux-x86_64")
	old.HostTools["python"] = "3.12.14"
	if err := checkHostTools(old, "linux-x86_64", linux); err == nil || !strings.Contains(err.Error(), "python 3.12.14") {
		t.Fatalf("version mismatch err %v", err)
	}
	missing := mustLock(t, "linux-x86_64")
	delete(missing.Platforms["linux-x86_64"].HostTools, "gperf")
	if err := checkHostTools(missing, "linux-x86_64", linux); err == nil || !strings.Contains(err.Error(), "host tools for linux-x86_64") {
		t.Fatalf("missing tool err %v", err)
	}
}

func TestHostToolsIDMatchesInstall(t *testing.T) {
	lock := mustLock(t, "linux-x86_64")
	for _, triple := range tools.PinnedTriples() {
		got, err := hostToolsID(lock, triple)
		if err != nil {
			t.Fatal(err)
		}
		want, err := tools.IDFor(triple)
		if err != nil || got != want {
			t.Fatalf("%s id %s, install uses %s (err %v)", triple, got, want, err)
		}
	}
	if _, err := hostToolsID(lock, "solaris-sparc"); err == nil {
		t.Fatal("unknown platform has an id")
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
