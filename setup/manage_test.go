package setup

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"windlock/host"
	"windlock/lockfile"
	"windlock/tools"
)

// managedProject is an initialised project on a fake root: both trees and SDKs
// present, each SDK with only the arm toolchain, sha256.sum from testSums.
func managedProject(t *testing.T) string {
	t.Helper()
	triple, err := host.Current()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := assembleLock(testEntries(), "1.5.0", testSums, triple); err != nil {
		t.Skipf("test entries cannot lock on %s: %v", triple, err)
	}
	stub := fetchSums
	fetchSums = func([]string, io.Writer) (map[string]string, error) { return testSums, nil }
	t.Cleanup(func() { fetchSums = stub })

	root := t.TempDir()
	t.Setenv(tools.RootEnv, root)
	entries := testEntries()
	for i := range entries {
		entries[i].Toolchains = []string{}
		e := entries[i]
		mkdir(t, filepath.Join(root, "workspaces", e.Commit, "zephyr"))
		sdkDir := filepath.Join(root, "sdk", e.SDK, triple, "zephyr-sdk-"+e.SDK)
		mkdir(t, filepath.Join(sdkDir, "gnu", "arm-zephyr-eabi"))
		writeFile(t, filepath.Join(sdkDir, "sdk_version"), e.SDK+"\n")
	}
	lock, err := assembleLock(entries, "1.5.0", testSums, triple)
	if err != nil {
		t.Fatal(err)
	}

	app := t.TempDir()
	for board, arch := range map[string]string{"acme_board": "arm", "acme_x86": "x86"} {
		writeBoard(t, filepath.Join(app, "boards"), board, arch)
	}
	// A board without its Twister file.
	writeFile(t, filepath.Join(app, "boards", "acme_bare", "board.yml"), "board:\n  name: acme_bare\n  socs:\n  - name: s\n")
	mkdir(t, filepath.Join(app, "apps", "app"))
	mkdir(t, filepath.Join(app, "apps", "sensor"))
	if err := lockfile.WriteConfigFile(filepath.Join(app, lockfile.ConfigFilename), lockfile.Config{Zephyr: []string{"v4.4.2", "v4.3.1"}}); err != nil {
		t.Fatal(err)
	}
	if err := lockfile.WriteFile(filepath.Join(app, lockfile.Filename), lock); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestTargetCommands(t *testing.T) {
	app := managedProject(t)
	if err := TargetAdd(app, "app", "acme_board", "apps/app", TargetOptions{}, io.Discard); err != nil {
		t.Fatal(err)
	}
	cfg, lock := readProject(t, app)
	if cfg.Target != "app" || cfg.Targets["app"] != (lockfile.Target{Board: "acme_board", Source: "apps/app"}) {
		t.Fatalf("config %+v", cfg)
	}
	for _, e := range lock.Zephyr {
		if !reflect.DeepEqual(e.Toolchains, []string{"arm-zephyr-eabi"}) {
			t.Fatalf("zephyr %s toolchains %v", e.Name, e.Toolchains)
		}
	}

	if err := TargetAdd(app, "sensor", "acme_board", "./apps/sensor/", TargetOptions{Default: true}, io.Discard); err != nil {
		t.Fatal(err)
	}
	cfg, _ = readProject(t, app)
	if cfg.Target != "sensor" || cfg.Targets["sensor"].Source != "apps/sensor" {
		t.Fatalf("--default or source cleanup: %+v", cfg)
	}
	var out bytes.Buffer
	if err := TargetList(app, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if want := "  app  acme_board  apps/app\n* sensor  acme_board  apps/sensor\n"; out.String() != want {
		t.Fatalf("list %q, want %q", out.String(), want)
	}

	if err := TargetDefault(app, "app", io.Discard); err != nil {
		t.Fatal(err)
	}
	if cfg, _ = readProject(t, app); cfg.Target != "app" {
		t.Fatalf("default %q", cfg.Target)
	}
	if err := TargetRemove(app, "app", io.Discard); err != nil {
		t.Fatal(err)
	}
	cfg, lock = readProject(t, app)
	if cfg.Target != "" || len(cfg.Targets) != 1 || len(lock.Zephyr[0].Toolchains) != 1 {
		t.Fatalf("after removing the default: %+v %v", cfg, lock.Zephyr[0].Toolchains)
	}
	if err := TargetRemove(app, "sensor", io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, lock = readProject(t, app); len(lock.Zephyr[0].Toolchains) != 0 {
		t.Fatalf("toolchains left after the last target: %v", lock.Zephyr[0].Toolchains)
	}
}

// A refused change writes neither file.
func TestTargetAddFailuresWriteNothing(t *testing.T) {
	app := managedProject(t)
	before := snapshotProject(t, app)
	cases := []struct {
		name, board, source, want string
	}{
		{"app", "acme_nope", "apps/app", "board acme_nope not found"},
		{"app", "acme_board", "apps/missing", "no such file"},
		{"app", "acme_board", "../elsewhere", "outside"},
		{"app", "acme_board", "/abs", "relative"},
		{"a/b", "acme_board", "apps/app", "letters"},
		// x86 needs a toolchain the fake SDK lacks, and there are no host
		// tools to install it with.
		{"x86", "acme_x86", "apps/app", "host tools bin"},
		{"bare", "acme_bare", "apps/app", "acme_bare.yaml containing"},
	}
	for _, c := range cases {
		err := TargetAdd(app, c.name, c.board, c.source, TargetOptions{}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s %s %s: err %v, want %q", c.name, c.board, c.source, err, c.want)
		}
		if after := snapshotProject(t, app); after != before {
			t.Fatalf("%s %s %s changed the project files", c.name, c.board, c.source)
		}
	}
	if err := TargetRemove(app, "app", io.Discard); err == nil {
		t.Fatal("removed a target that does not exist")
	}
	if err := TargetDefault(app, "app", io.Discard); err == nil {
		t.Fatal("defaulted to a target that does not exist")
	}
}

func TestZephyrCommands(t *testing.T) {
	app := managedProject(t)
	if err := ZephyrDefault(app, "v4.3.1", io.Discard); err != nil {
		t.Fatal(err)
	}
	cfg, lock := readProject(t, app)
	if !reflect.DeepEqual(cfg.Zephyr, []string{"v4.3.1", "v4.4.2"}) || lock.Zephyr[0].Name != "4.3.1" {
		t.Fatalf("after default: %v %s", cfg.Zephyr, lock.Zephyr[0].Name)
	}
	var out bytes.Buffer
	if err := ZephyrList(app, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if want := "* 4.3.1  v4.3.1  75f67d76  sdk 0.17.4\n  4.4.2  v4.4.2  dccb0959  sdk 1.0.1\n"; out.String() != want {
		t.Fatalf("list %q, want %q", out.String(), want)
	}
	if err := ZephyrRemove(app, "4.3.1", io.Discard); err == nil || !strings.Contains(err.Error(), "default") {
		t.Fatalf("removing the default err %v", err)
	}
	if err := ZephyrRemove(app, "4.4.2", io.Discard); err != nil {
		t.Fatal(err)
	}
	cfg, lock = readProject(t, app)
	if !reflect.DeepEqual(cfg.Zephyr, []string{"v4.3.1"}) || len(lock.Zephyr) != 1 {
		t.Fatalf("after remove: %v %+v", cfg.Zephyr, lock.Zephyr)
	}
	for triple, p := range lock.Platforms {
		if _, ok := p.SDK["1.0.1"]; ok {
			t.Fatalf("%s still locks SDK 1.0.1, which no entry uses", triple)
		}
	}
	if err := ZephyrRemove(app, "v9.9.9", io.Discard); err == nil || !strings.Contains(err.Error(), "no zephyr v9.9.9") {
		t.Fatalf("unknown revision err %v", err)
	}
}

func TestManagedRefusesStalePair(t *testing.T) {
	app := managedProject(t)
	cfgPath := filepath.Join(app, lockfile.ConfigFilename)
	if err := lockfile.WriteConfigFile(cfgPath, lockfile.Config{Zephyr: []string{"v4.4.2"}}); err != nil {
		t.Fatal(err)
	}
	if err := TargetList(app, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "windlock init") {
		t.Fatalf("stale pair err %v", err)
	}
	if err := TargetList(t.TempDir(), io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "windlock init first") {
		t.Fatalf("no project err %v", err)
	}
}

func readProject(t *testing.T, app string) (lockfile.Config, lockfile.Lockfile) {
	t.Helper()
	cfg, err := lockfile.ReadConfigFile(filepath.Join(app, lockfile.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockfile.ReadFile(filepath.Join(app, lockfile.Filename))
	if err != nil {
		t.Fatal(err)
	}
	return cfg, lock
}

func snapshotProject(t *testing.T, app string) string {
	t.Helper()
	var b strings.Builder
	for _, name := range []string{lockfile.ConfigFilename, lockfile.Filename} {
		raw, err := os.ReadFile(filepath.Join(app, name))
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
	}
	return b.String()
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTargetAddSysbuild(t *testing.T) {
	app := managedProject(t)
	writeFile(t, filepath.Join(app, "apps", "app", "sysbuild.conf"), "SB_CONFIG_BOOTLOADER_MCUBOOT=y\n")
	var log bytes.Buffer
	if err := TargetAdd(app, "app", "acme_board", "apps/app", TargetOptions{}, &log); err != nil {
		t.Fatal(err)
	}
	cfg, _ := readProject(t, app)
	if !cfg.Targets["app"].Sysbuild || !strings.Contains(log.String(), "sysbuild on (found apps/app/sysbuild.conf)") {
		t.Fatalf("detected: %+v, log %q", cfg.Targets["app"], log.String())
	}
	off := false
	if err := TargetAdd(app, "app_plain", "acme_board", "apps/app", TargetOptions{Sysbuild: &off}, io.Discard); err != nil {
		t.Fatal(err)
	}
	on := true
	if err := TargetAdd(app, "sensor", "acme_board", "apps/sensor", TargetOptions{Sysbuild: &on}, io.Discard); err != nil {
		t.Fatal(err)
	}
	cfg, _ = readProject(t, app)
	if cfg.Targets["app_plain"].Sysbuild || !cfg.Targets["sensor"].Sysbuild {
		t.Fatalf("overrides: %+v", cfg.Targets)
	}
	var out bytes.Buffer
	if err := TargetList(app, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	want := "* app  acme_board  apps/app  sysbuild\n  app_plain  acme_board  apps/app\n  sensor  acme_board  apps/sensor  sysbuild\n"
	if out.String() != want {
		t.Fatalf("list %q, want %q", out.String(), want)
	}
}

func TestDetectSysbuildMarkers(t *testing.T) {
	for _, marker := range []string{"sysbuild.conf", "Kconfig.sysbuild", "sysbuild/CMakeLists.txt"} {
		app := t.TempDir()
		writeFile(t, filepath.Join(app, "apps", "a", filepath.FromSlash(marker)), "")
		if on, why := detectSysbuild(app, "apps/a"); !on || why == "" {
			t.Fatalf("%s not detected", marker)
		}
	}
	app := t.TempDir()
	mkdir(t, filepath.Join(app, "apps", "a"))
	if on, why := detectSysbuild(app, "apps/a"); on || why != "" {
		t.Fatal("plain app detected as sysbuild")
	}
}
