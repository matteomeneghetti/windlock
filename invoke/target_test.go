package invoke

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"windlock/internal/envvar"
	"windlock/lockfile"
	"windlock/setup"
	"windlock/tools"
)

const (
	testCommit = "dccb09599635bdff17633fa7e9dab014b91dce90"
	testSDK    = "1.0.1"
	testTriple = "linux-x86_64"
)

// testHostID is the host-tools id testLock selects: one cmake pin.
var testHostID = tools.ID([]tools.Version{{Name: "cmake", Version: "3.31.12"}})

func testConfig() lockfile.Config {
	return lockfile.Config{
		Zephyr: []string{"v4.4.1"},
		Target: "app",
		Targets: map[string]lockfile.Target{
			"app":    {Board: "acme_board", Source: "apps/app"},
			"sensor": {Board: "acme_sensor", Source: "apps/sensor"},
		},
	}
}

func testLock() lockfile.Lockfile {
	return lockfile.Lockfile{
		Schema:    lockfile.Schema,
		HostTools: map[string]string{"cmake": "3.31.12"},
		Zephyr: []lockfile.Zephyr{{
			Name:   "4.4.1",
			Commit: testCommit,
			SDK:    testSDK,
		}},
		Platforms: map[string]lockfile.Platform{
			testTriple: {
				HostTools: map[string]lockfile.Archive{"cmake": {URL: "u", SHA256: "s"}},
				SDK:       map[string]lockfile.Bundle{testSDK: {Filename: "zephyr-sdk-1.0.1_linux-x86_64_minimal.tar.xz", SHA256: "abc"}},
			},
		},
	}
}

func makeLayout(t *testing.T, root, app, ws string) string {
	t.Helper()
	sdkDir := filepath.Join(root, "sdk", testSDK, testTriple, "zephyr-sdk-"+testSDK)
	for _, dir := range []string{
		filepath.Join(app, "apps", "app"),
		filepath.Join(app, "apps", "sensor"),
		filepath.Join(ws, "zephyr"),
		filepath.Join(ws, ".venv", "bin"),
		filepath.Join(root, "host", testHostID, "bin"),
		sdkDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(sdkDir, "sdk_version"), []byte(testSDK+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return sdkDir
}

func TestPrepareBuild(t *testing.T) {
	root := t.TempDir()
	app := t.TempDir()
	ws := filepath.Join(root, "workspaces", testCommit)
	sdkDir := makeLayout(t, root, app, ws)
	build, err := buildCommand(testConfig(), testLock(), root, testTriple, app, "/opt/homebrew/bin:/usr/bin", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(build.Args, " ") != "build -b acme_board -d build/app apps/app" {
		t.Fatalf("build argv %q", build.Args)
	}
	if build.Dir != app {
		t.Fatalf("dir %s", build.Dir)
	}
	for _, arg := range build.Args {
		if strings.HasPrefix(arg, "-G") {
			t.Fatalf("generator flag in %q", build.Args)
		}
	}
	if envvar.Get(build.Env, "ZEPHYR_TOOLCHAIN_VARIANT") != "zephyr" {
		t.Fatal("variant")
	}
	if got := envvar.Get(build.Env, "ZEPHYR_SDK_INSTALL_DIR"); got != sdkDir {
		t.Fatalf("sdk dir %s", got)
	}
	if envvar.Get(build.Env, "ZEPHYR_BASE") != filepath.Join(ws, "zephyr") {
		t.Fatal("zephyr base")
	}
	if envvar.Get(build.Env, "VIRTUAL_ENV") != filepath.Join(ws, ".venv") {
		t.Fatal("virtual env")
	}
	prefix := filepath.Join(ws, ".venv", "bin") + string(os.PathListSeparator) +
		filepath.Join(root, "host", testHostID, "bin") + string(os.PathListSeparator)
	if path := envvar.Get(build.Env, "PATH"); !strings.HasPrefix(path, prefix) {
		t.Fatalf("PATH %s", path)
	}
}

func TestManifestBuildOmitsCMakeFlags(t *testing.T) {
	root := t.TempDir()
	app := t.TempDir()
	if err := os.WriteFile(filepath.Join(app, "west.yml"), []byte("manifest:\n  self:\n    path: my-firmware\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := setup.WorkspaceDir(root, app, testCommit)
	if err != nil {
		t.Fatal(err)
	}
	makeLayout(t, root, app, ws)
	cmd, err := buildCommand(testConfig(), testLock(), root, testTriple, app, "/usr/bin", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(cmd.Args, " ")
	if got != "build -b acme_board -d build/app apps/app" {
		t.Fatalf("argv %q", cmd.Args)
	}
	if strings.Contains(got, "BOARD_ROOT") || strings.Contains(got, "ZEPHYR_EXTRA_MODULES") || strings.Contains(got, " -- ") {
		t.Fatalf("cmake flags in %q", cmd.Args)
	}
	if envvar.Get(cmd.Env, "ZEPHYR_BASE") != filepath.Join(ws, "zephyr") {
		t.Fatalf("base %s", envvar.Get(cmd.Env, "ZEPHYR_BASE"))
	}
}

func TestBuildSelectsTarget(t *testing.T) {
	root := t.TempDir()
	app := t.TempDir()
	makeLayout(t, root, app, filepath.Join(root, "workspaces", testCommit))
	cfg := testConfig()
	named, err := buildCommand(cfg, testLock(), root, testTriple, app, "/usr/bin", "sensor", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(named.Args, " ") != "build -b acme_sensor -d build/sensor apps/sensor" {
		t.Fatalf("named argv %q", named.Args)
	}
	if _, err := buildCommand(cfg, testLock(), root, testTriple, app, "/usr/bin", "missing", nil); err == nil || !strings.Contains(err.Error(), lockfile.ConfigFilename) {
		t.Fatalf("missing target err %v", err)
	}
	cfg.Target = ""
	if _, err := buildCommand(cfg, testLock(), root, testTriple, app, "/usr/bin", "", nil); err == nil || !strings.Contains(err.Error(), "target is not set") {
		t.Fatalf("unset target err %v", err)
	}
}

func TestPrepareRejectsMissingSource(t *testing.T) {
	root := t.TempDir()
	app := t.TempDir()
	makeLayout(t, root, app, filepath.Join(root, "workspaces", testCommit))
	cfg := testConfig()
	cfg.Targets["app"] = lockfile.Target{Board: "acme_board", Source: "samples/hello_world"}
	if _, err := buildCommand(cfg, testLock(), root, testTriple, app, "/usr/bin", "", nil); err == nil || !strings.Contains(err.Error(), `target "app" source`) {
		t.Fatalf("err %v", err)
	}
}

func TestPrepareRejectsMissingPlatformAndBundle(t *testing.T) {
	_, err := buildCommand(testConfig(), testLock(), t.TempDir(), "macos-x86_64", t.TempDir(), "", "", nil)
	if err == nil || !strings.Contains(err.Error(), "macos-x86_64") {
		t.Fatalf("missing platform err %v", err)
	}
	lock := testLock()
	p := lock.Platforms[testTriple]
	p.SDK = nil
	lock.Platforms[testTriple] = p
	_, err = buildCommand(testConfig(), lock, t.TempDir(), testTriple, t.TempDir(), "", "", nil)
	if err == nil || !strings.Contains(err.Error(), "SDK 1.0.1") {
		t.Fatalf("missing bundle err %v", err)
	}
}

func TestPrepareForwardsBuildArguments(t *testing.T) {
	root := t.TempDir()
	app := t.TempDir()
	makeLayout(t, root, app, filepath.Join(root, "workspaces", testCommit))
	cases := map[string]string{
		"":                             "build -b acme_board -d build/app apps/app",
		"-p always":                    "build -b acme_board -d build/app -p always apps/app",
		"-- -DEXTRA_CONF_FILE=x.conf":  "build -b acme_board -d build/app apps/app -- -DEXTRA_CONF_FILE=x.conf",
		"-p always -- -DFOO=1 -DBAR=2": "build -b acme_board -d build/app -p always apps/app -- -DFOO=1 -DBAR=2",
		// After --, arguments go to CMake, so -d there is not west's -d.
		"-- -d": "build -b acme_board -d build/app apps/app -- -d",
	}
	for extra, want := range cases {
		cmd, err := buildCommand(testConfig(), testLock(), root, testTriple, app, "", "", strings.Fields(extra))
		if err != nil {
			t.Fatalf("%q: %v", extra, err)
		}
		if got := strings.Join(cmd.Args, " "); got != want {
			t.Fatalf("%q: argv %q, want %q", extra, got, want)
		}
	}
}

func TestPrepareRejectsOwnedOptions(t *testing.T) {
	root := t.TempDir()
	app := t.TempDir()
	makeLayout(t, root, app, filepath.Join(root, "workspaces", testCommit))
	for _, opt := range []string{"-b", "--board", "--board=x", "-bnrf", "-d", "--build-dir", "--build-dir=out", "-dout", "-s", "--source-dir=x"} {
		_, err := buildCommand(testConfig(), testLock(), root, testTriple, app, "", "", []string{opt, "x"})
		if err == nil || !strings.Contains(err.Error(), "set by target app") {
			t.Fatalf("%s: err %v", opt, err)
		}
	}
}

func TestPrepareSysbuildTarget(t *testing.T) {
	root := t.TempDir()
	app := t.TempDir()
	makeLayout(t, root, app, filepath.Join(root, "workspaces", testCommit))
	cfg := testConfig()
	cfg.Targets["app"] = lockfile.Target{Board: "acme_board", Source: "apps/app", Sysbuild: true}
	cases := map[string]string{
		"":                       "build -b acme_board -d build/app --sysbuild apps/app",
		"-p always -- -DFOO=1":   "build -b acme_board -d build/app --sysbuild -p always apps/app -- -DFOO=1",
		"--no-sysbuild":          "build -b acme_board -d build/app --no-sysbuild apps/app",
		"--sysbuild":             "build -b acme_board -d build/app --sysbuild apps/app",
		"-- --no-sysbuild-cmake": "build -b acme_board -d build/app --sysbuild apps/app -- --no-sysbuild-cmake",
	}
	for extra, want := range cases {
		cmd, err := buildCommand(cfg, testLock(), root, testTriple, app, "", "", strings.Fields(extra))
		if err != nil {
			t.Fatalf("%q: %v", extra, err)
		}
		if got := strings.Join(cmd.Args, " "); got != want {
			t.Fatalf("%q: argv %q, want %q", extra, got, want)
		}
	}
}
