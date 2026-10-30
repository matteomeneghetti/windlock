package invoke

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"windlock/host"
	"windlock/lockfile"
	"windlock/tools"
)

// fakeProject is an initialised project on a fake root whose west records
// its args and env under rec, emulates west config --local, and exits 3.
type fakeProject struct {
	root, app, ws, rec string
}

func newFakeProject(t *testing.T) fakeProject {
	t.Helper()
	triple, err := host.Current()
	if err != nil {
		t.Fatal(err)
	}
	p := fakeProject{root: t.TempDir(), app: t.TempDir(), rec: t.TempDir()}
	p.ws = filepath.Join(p.root, "workspaces", testCommit)
	t.Setenv(tools.RootEnv, p.root)

	lock := testLock()
	lock.Platforms = map[string]lockfile.Platform{triple: lock.Platforms[testTriple]}
	if err := lockfile.WriteFile(filepath.Join(p.app, lockfile.Filename), lock); err != nil {
		t.Fatal(err)
	}
	if err := lockfile.WriteConfigFile(filepath.Join(p.app, lockfile.ConfigFilename), testConfig()); err != nil {
		t.Fatal(err)
	}
	sdkDir := filepath.Join(p.root, "sdk", testSDK, triple, "zephyr-sdk-"+testSDK)
	for _, dir := range []string{filepath.Join(p.app, "apps", "app"), filepath.Join(p.ws, "zephyr"), filepath.Join(p.ws, ".west"),
		filepath.Join(p.ws, ".venv", "bin"), filepath.Join(p.root, "host", testHostID, "bin"), sdkDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(sdkDir, "sdk_version"), testSDK+"\n", 0o644)
	writeTestFile(t, filepath.Join(p.ws, ".west", "config"), "[manifest]\npath = zephyr\n", 0o644)
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = config ]; then\n" +
		"  echo config >> '" + p.rec + "/config-calls'\n" +
		"  printf '[build]\\ncmake-args = %s\\n' \"$5\" >> '" + p.ws + "/.west/config'\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s\\n' \"$@\" > '" + p.rec + "/args'\n" +
		"env > '" + p.rec + "/env'\n" +
		"pwd > '" + p.rec + "/pwd'\n" +
		"exit 3\n"
	writeTestFile(t, filepath.Join(p.ws, ".venv", "bin", "west"), script, 0o755)
	return p
}

func (p fakeProject) read(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(p.rec, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func writeTestFile(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}

func TestWestRunsIsolated(t *testing.T) {
	p := newFakeProject(t)
	var out, errOut bytes.Buffer
	err := West(p.app, []string{"flash", "--runner", "jlink"}, Streams{Out: &out, ErrOut: &errOut})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 3 {
		t.Fatalf("err %v, want exit 3", err)
	}
	if got := p.read(t, "args"); got != "flash\n--runner\njlink\n" {
		t.Fatalf("args %q", got)
	}
	if got := strings.TrimSpace(p.read(t, "pwd")); filepath.Base(got) != filepath.Base(p.app) {
		t.Fatalf("ran in %s, want the project", got)
	}
	env := p.read(t, "env")
	for _, want := range []string{
		"ZEPHYR_BASE=" + filepath.Join(p.ws, "zephyr"),
		"VIRTUAL_ENV=" + filepath.Join(p.ws, ".venv"),
		"ZEPHYR_TOOLCHAIN_VARIANT=zephyr",
		"PYTHONNOUSERSITE=1",
		"PIP_CACHE_DIR=" + filepath.Join(p.root, "cache", "pip"),
		"CCACHE_DIR=" + filepath.Join(p.root, "cache", "ccache"),
		"XDG_CACHE_HOME=" + filepath.Join(p.root, "cache", "xdg"),
	} {
		if !strings.Contains(env, want+"\n") {
			t.Fatalf("env lacks %s", want)
		}
	}
	// windlock no longer edits the workspace config; XDG_CACHE_HOME covers
	// Zephyr's cache, sysbuild images included, on Linux.
	if config, _ := os.ReadFile(filepath.Join(p.ws, ".west", "config")); string(config) != "[manifest]\npath = zephyr\n" {
		t.Fatalf("workspace config changed:\n%s", config)
	}
}

func TestWestRefusesCommandsThatLeak(t *testing.T) {
	p := newFakeProject(t)
	for args, want := range map[string]string{
		"zephyr-export":     ".cmake/packages",
		"sdk install":       "outside the toolchain root",
		"-v update":         "windlock sync",
		"init -m somewhere": "windlock zephyr add",
	} {
		err := West(p.app, strings.Fields(args), Streams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("west %s: err %v, want %q", args, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(p.rec, "args")); err == nil {
		t.Fatal("a refused command reached west")
	}
}

func TestShellRunsInTheEnvironment(t *testing.T) {
	p := newFakeProject(t)
	t.Setenv("SHELL", filepath.Join(p.ws, ".venv", "bin", "west"))
	var errOut bytes.Buffer
	err := Shell(p.app, []string{"-c", "true"}, Streams{Out: &bytes.Buffer{}, ErrOut: &errOut})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 3 {
		t.Fatalf("err %v", err)
	}
	if got := p.read(t, "args"); got != "-c\ntrue\n" {
		t.Fatalf("shell args %q", got)
	}
	if !strings.Contains(p.read(t, "env"), "WINDLOCK_PROJECT="+p.app+"\n") {
		t.Fatal("WINDLOCK_PROJECT not set")
	}
	if !strings.Contains(errOut.String(), "zephyr-export") {
		t.Fatalf("shell did not state the rule: %q", errOut.String())
	}
}
