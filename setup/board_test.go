package setup

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/matteomeneghetti/windlock/lockfile"
)

func TestReadSDKVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SDK_VERSION"), []byte("0.17.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readSDKVersion(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.17.4" {
		t.Fatalf("SDK_VERSION %s", got)
	}
}

func TestToolchainForArch(t *testing.T) {
	cases := []struct {
		arch string
		want string
	}{
		{"arm", "arm-zephyr-eabi"},
		{"x86", "x86_64-zephyr-elf"},
		{"riscv", "riscv64-zephyr-elf"},
		{"arm64", "aarch64-zephyr-elf"},
		{"aarch64", "aarch64-zephyr-elf"},
	}
	for _, tc := range cases {
		got, err := toolchainForArch(tc.arch)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("%s -> %s, want %s", tc.arch, got, tc.want)
		}
	}
	if _, err := toolchainForArch("arc"); err == nil {
		t.Fatal("unknown arch was accepted")
	}
}

func TestLoadConfigCreatesDefault(t *testing.T) {
	app := t.TempDir()
	cfg, prev, err := loadConfig(app, "", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Zephyr, []string{zephyrRevision}) || len(cfg.Targets) != 0 || prev.West != "" {
		t.Fatalf("config %+v west %q", cfg, prev.West)
	}
	onDisk, err := lockfile.ReadConfigFile(filepath.Join(app, lockfile.ConfigFilename))
	if err != nil || !reflect.DeepEqual(onDisk, cfg) {
		t.Fatalf("windlock.yml %+v err %v", onDisk, err)
	}
	if err := os.WriteFile(filepath.Join(app, "west.yml"), []byte("manifest:\n  self:\n    path: fw\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadConfig(app, "", io.Discard); err == nil || !strings.Contains(err.Error(), "remove zephyr") {
		t.Fatalf("manifest with revisions err %v", err)
	}
}

func TestParseFreeze(t *testing.T) {
	const freeze = `manifest:
  projects:
  - name: cmsis_6
    url: https://github.com/zephyrproject-rtos/cmsis_6
    revision: 06d952b6713a2ca41c9224a62075e4059402a151
    path: modules/hal/cmsis_6
  - name: picolibc
    url: https://github.com/zephyrproject-rtos/picolibc
    revision: 560946f26db075c296beea5b39d99e6de43c9010
  self:
    path: zephyr
`
	got, err := parseFreeze(freeze)
	if err != nil {
		t.Fatal(err)
	}
	want := []lockfile.Project{
		{Name: "cmsis_6", Path: "modules/hal/cmsis_6", Revision: "06d952b6713a2ca41c9224a62075e4059402a151"},
		{Name: "picolibc", Path: "picolibc", Revision: "560946f26db075c296beea5b39d99e6de43c9010"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projects %+v", got)
	}
	if _, err := parseFreeze("manifest:\n  projects:\n  - name: x\n    revision: main\n"); err == nil || !strings.Contains(err.Error(), "main") {
		t.Fatalf("branch revision err %v", err)
	}
}

func TestInstallBoardToolchainRefusesUnlockedToolchain(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WINDLOCK_ROOT", root)
	app := t.TempDir()
	cfg := lockfile.Config{Zephyr: []string{"v4.4.2"}, Target: "x", Targets: map[string]lockfile.Target{"x": {Board: "acme_x86", Source: "app"}}}
	if err := lockfile.WriteConfigFile(filepath.Join(app, lockfile.ConfigFilename), cfg); err != nil {
		t.Fatal(err)
	}
	if err := lockfile.WriteFile(filepath.Join(app, lockfile.Filename), mustLock(t, "linux-x86_64")); err != nil {
		t.Fatal(err)
	}
	writeBoard(t, filepath.Join(app, "boards"), "acme_x86", "x86")
	err := InstallBoardToolchain(app, "", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "x86_64-zephyr-elf") || !strings.Contains(err.Error(), "windlock init") {
		t.Fatalf("err %v", err)
	}
}

func TestInstallSDKEmptyToolchains(t *testing.T) {
	dir := t.TempDir()
	west := filepath.Join(dir, "west")
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + argsFile + "\"\nexit 0\n"
	if err := os.WriteFile(west, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir()) // cleanCMake snapshots ~/.cmake/packages
	h := hostEnv{root: dir, triple: "linux-x86_64", hostBin: dir, log: io.Discard}
	err := installSDK(h, workspace{dir: dir, west: west}, "1.0.1", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "--no-gnu-toolchains") {
		t.Fatalf("args %q", text)
	}
	if strings.Contains(text, "no toolchains to install") {
		t.Fatalf("args %q", text)
	}
}

func TestMissingToolchainLooksUnderGnu(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "gnu", "arm-zephyr-eabi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "x86_64-zephyr-elf"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := missingToolchains(dir, []string{"arm-zephyr-eabi", "x86_64-zephyr-elf", "riscv64-zephyr-elf"})
	if !reflect.DeepEqual(got, []string{"riscv64-zephyr-elf"}) {
		t.Fatalf("missing %v", got)
	}
}

func TestParseAddRevision(t *testing.T) {
	name, ref, err := parseAddRevision("v4.4.1")
	if err != nil {
		t.Fatal(err)
	}
	if name != "4.4.1" || ref != "v4.4.1" {
		t.Fatalf("name %s ref %s", name, ref)
	}
	name, ref, err = parseAddRevision("main")
	if err != nil {
		t.Fatal(err)
	}
	if name != "main" || ref != "main" {
		t.Fatalf("name %s ref %s", name, ref)
	}
	if _, _, err := parseAddRevision("  "); err == nil {
		t.Fatal("empty revision was accepted")
	}
}

func TestLoadConfigZephyrFlag(t *testing.T) {
	app := t.TempDir()
	cfg, _, err := loadConfig(app, "v4.3.1", io.Discard)
	if err != nil || !reflect.DeepEqual(cfg.Zephyr, []string{"v4.3.1"}) {
		t.Fatalf("new windlock.yml %+v err %v", cfg, err)
	}
	if _, _, err := loadConfig(app, "v4.3.1", io.Discard); err == nil || !strings.Contains(err.Error(), "windlock zephyr add") {
		t.Fatalf("--zephyr with windlock.yml err %v", err)
	}
	if again, _, err := loadConfig(app, "", io.Discard); err != nil || !reflect.DeepEqual(again, cfg) {
		t.Fatalf("rerun %+v err %v", again, err)
	}
	manifest := t.TempDir()
	if err := os.WriteFile(filepath.Join(manifest, "west.yml"), []byte("manifest:\n  self:\n    path: fw\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadConfig(manifest, "v4.3.1", io.Discard); err == nil || !strings.Contains(err.Error(), "west.yml") {
		t.Fatalf("--zephyr in a west.yml project err %v", err)
	}
}

func TestLoadConfigReplacesUnreadableOldLockfile(t *testing.T) {
	app := t.TempDir()
	lockPath := filepath.Join(app, lockfile.Filename)
	if err := os.WriteFile(lockPath, []byte("schema: 3\nwest: 1.5.0\nold_field: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	cfg, prev, err := loadConfig(app, "", &log)
	if err != nil {
		t.Fatalf("pre-release lockfile err %v", err)
	}
	if prev.West != "" || len(prev.Zephyr) != 0 || len(cfg.Zephyr) != 1 {
		t.Fatalf("config %+v prev %+v", cfg, prev)
	}
	if !strings.Contains(log.String(), "writing a new one") {
		t.Fatalf("log %q", log.String())
	}
	// A newer lockfile is never replaced: that would downgrade it.
	if err := os.WriteFile(lockPath, []byte("schema: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadConfig(app, "", io.Discard); err == nil || !strings.Contains(err.Error(), "upgrade windlock") {
		t.Fatalf("newer lockfile err %v", err)
	}
}
