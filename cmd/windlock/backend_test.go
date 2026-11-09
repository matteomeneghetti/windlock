package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/matteomeneghetti/windlock/container"
	"github.com/matteomeneghetti/windlock/lockfile"
	"github.com/matteomeneghetti/windlock/tools"
)

// containerProject switches to the container backend with a fake engine
// that records its argv and exits with $FAKE_EXIT, in a fresh project dir.
func containerProject(t *testing.T) (argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "engine-args")
	engine := filepath.Join(dir, "engine")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + argsFile + "\"\nexit ${FAKE_EXIT:-0}\n"
	if err := os.WriteFile(engine, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(container.EngineEnv, engine)
	t.Setenv(tools.RootEnv, filepath.Join(dir, "root"))
	t.Setenv("WINDLOCK_IN_CONTAINER", "")
	oldBin, oldVol := linuxBinary, ensureVolume
	linuxBinary = func(string) (string, error) { return "/fake/windlock-linux", nil }
	ensureVolume = func(container.Engine, string, string, string, int, int) error { return nil }
	t.Cleanup(func() { linuxBinary, ensureVolume = oldBin, oldVol })
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(dir, "app")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	return argsFile
}

func engineArgs(t *testing.T, argsFile string) []string {
	t.Helper()
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("engine did not run: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

func TestContainerBackendRoutesBuild(t *testing.T) {
	argsFile := containerProject(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-v", "build", "app", "-p", "always"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d stderr %q", code, stderr.String())
	}
	args := engineArgs(t, argsFile)
	joined := strings.Join(args, " ")
	wd, _ := os.Getwd()
	for _, want := range []string{
		"run --rm -i",
		"-v " + wd + ":" + wd + " -w " + wd,
		"-v /fake/windlock-linux:/usr/local/bin/windlock:ro",
		"--entrypoint /usr/local/bin/windlock " + tools.ContainerImage + " -v build app -p always",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("engine args %q lack %q", joined, want)
		}
	}
	vol := container.VolumeName(os.Getenv(tools.RootEnv), mustTriple(t))
	if !strings.Contains(joined, "-v "+vol+":/windlock/root") {
		t.Fatalf("engine args %q lack the volume %s", joined, vol)
	}
}

func TestContainerBackendUsesLockfileImage(t *testing.T) {
	argsFile := containerProject(t)
	lock := lockfile.Lockfile{Schema: lockfile.Schema, Container: lockfile.Container{Image: "example/image@sha256:1234"}}
	if err := lockfile.WriteFile(lockfile.Filename, lock); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"sync"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d stderr %q", code, stderr.String())
	}
	if joined := strings.Join(engineArgs(t, argsFile), " "); !strings.Contains(joined, "example/image@sha256:1234 sync") {
		t.Fatalf("engine args %q", joined)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv(tools.RootEnv), ".lock")); err != nil {
		t.Fatalf("sync did not take the root lock: %v", err)
	}
}

func TestContainerBackendPassesExitCode(t *testing.T) {
	containerProject(t)
	t.Setenv("FAKE_EXIT", "3")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"west", "boards"}, nil, &stdout, &stderr); code != 3 {
		t.Fatalf("exit %d stderr %q", code, stderr.String())
	}
}

func TestContainerBackendRefusesOnHost(t *testing.T) {
	argsFile := containerProject(t)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"target"}, "usage:"},
		{[]string{"target", "add", "app", "--board", "x"}, "usage:"},
		{[]string{"zephyr", "upgrade", "v4.4.2"}, `unknown zephyr command "upgrade"`},
		{[]string{"init", "extra"}, "usage:"},
	}
	for _, c := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(c.args, nil, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), c.want) {
			t.Fatalf("%v: exit %d stderr %q", c.args, code, stderr.String())
		}
	}
	if _, err := os.Stat(argsFile); err == nil {
		t.Fatal("the engine ran for a refused command")
	}
}

func mustTriple(t *testing.T) string {
	t.Helper()
	triple, err := container.Triple(runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	return triple
}

// hostFlashProject is a container-backend project whose west flash runs on
// the host through stand-ins: the flash tools, the copy out of the volume,
// and a venv whose west records its arguments and HOME.
func hostFlashProject(t *testing.T) (project, westLog string) {
	t.Helper()
	containerProject(t)
	project, _ = os.Getwd()
	root := os.Getenv(tools.RootEnv)
	lock := lockfile.Lockfile{Schema: lockfile.Schema, Zephyr: []lockfile.Zephyr{{
		Name: "4.4.2", Commit: strings.Repeat("a", 40), SDK: "1.0.1",
		PythonInputsSHA256: "in", PythonRequirements: []string{"west==1.5.0"},
	}}}
	if err := lockfile.WriteFile(lockfile.Filename, lock); err != nil {
		t.Fatal(err)
	}
	cfg := lockfile.Config{Zephyr: []string{"v4.4.2"}, Target: "app", Targets: map[string]lockfile.Target{"app": {Board: "acme_board", Source: "apps/app"}}}
	if err := lockfile.WriteConfigFile(lockfile.ConfigFilename, cfg); err != nil {
		t.Fatal(err)
	}
	runners := filepath.Join(project, "build", "app", "zephyr", "runners.yaml")
	if err := os.MkdirAll(filepath.Dir(runners), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runners, []byte("flash-runner: openocd\ndebug-runner: openocd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	westLog = filepath.Join(t.TempDir(), "west-args")
	bin := filepath.Join(root, "host", "id", "bin")
	oldInstall, oldCopy, oldSync := installFlash, copyZephyr, syncVenv
	installFlash = func(io.Writer) (string, error) { return bin, os.MkdirAll(bin, 0o755) }
	copyZephyr = func(_ string, _ lockfile.Zephyr, dst string) error {
		p := filepath.Join(dst, "scripts", "west-commands.yml")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, nil, 0o644)
	}
	syncVenv = func(_, _, venv string, _ []string, _ io.Writer) error {
		west := filepath.Join(venv, "bin", "west")
		if err := os.MkdirAll(filepath.Dir(west), 0o755); err != nil {
			return err
		}
		script := "#!/bin/sh\necho \"HOME=$HOME\" > " + westLog + "\nprintf '%s\\n' \"$@\" >> " + westLog + "\nexit ${FAKE_EXIT:-0}\n"
		return os.WriteFile(west, []byte(script), 0o755)
	}
	t.Cleanup(func() { installFlash, copyZephyr, syncVenv = oldInstall, oldCopy, oldSync })
	return project, westLog
}

func TestContainerBackendFlashesOnHost(t *testing.T) {
	project, westLog := hostFlashProject(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"west", "flash"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d stderr %q", code, stderr.String())
	}
	raw, err := os.ReadFile(westLog)
	if err != nil {
		t.Fatal(err)
	}
	root := os.Getenv(tools.RootEnv)
	got := strings.Split(strings.TrimSpace(string(raw)), "\n")
	want := []string{
		"HOME=" + filepath.Join(root, "flash", "home"),
		"flash", "-d", filepath.Join(project, "build", "app"), "--no-rebuild",
		"--openocd", filepath.Join(root, "host", "id", "bin", "openocd"),
		"--openocd-search", filepath.Join(root, "host", "id", "openocd", "scripts"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("west got\n %q\nwant\n %q", got, want)
	}
	// The exit code of the host west passes through.
	t.Setenv("FAKE_EXIT", "4")
	if code := run([]string{"west", "flash"}, nil, &stdout, &stderr); code != 4 {
		t.Fatalf("exit %d", code)
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func TestFlashBuildsThenFlashesOnHost(t *testing.T) {
	project, westLog := hostFlashProject(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"flash"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d stderr %q", code, stderr.String())
	}
	// The default target is built in the container first.
	engine := readLines(t, filepath.Join(filepath.Dir(os.Getenv(container.EngineEnv)), "engine-args"))
	if got := strings.Join(engine[len(engine)-2:], " "); got != "build app" {
		t.Fatalf("container ran %q", got)
	}
	root := os.Getenv(tools.RootEnv)
	want := []string{
		"HOME=" + filepath.Join(root, "flash", "home"),
		"flash", "-d", filepath.Join(project, "build", "app"), "--no-rebuild",
		"--openocd", filepath.Join(root, "host", "id", "bin", "openocd"),
		"--openocd-search", filepath.Join(root, "host", "id", "openocd", "scripts"),
	}
	if got := readLines(t, westLog); !reflect.DeepEqual(got, want) {
		t.Fatalf("west got\n %q\nwant\n %q", got, want)
	}
}

func TestFlashOptions(t *testing.T) {
	project, westLog := hostFlashProject(t)
	engineArgs := filepath.Join(filepath.Dir(os.Getenv(container.EngineEnv)), "engine-args")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"flash", "app", "--no-build", "-r", "dfu-util", "--", "--alt", "x"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d stderr %q", code, stderr.String())
	}
	if _, err := os.Stat(engineArgs); err == nil {
		t.Fatal("--no-build still built")
	}
	want := []string{"flash", "-d", filepath.Join(project, "build", "app"), "-r", "dfu-util", "--alt", "x", "--no-rebuild"}
	if got := readLines(t, westLog)[1:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("west got\n %q\nwant\n %q", got, want)
	}
}

func TestFlashStopsBeforeTheProbe(t *testing.T) {
	_, westLog := hostFlashProject(t)
	engineArgs := filepath.Join(filepath.Dir(os.Getenv(container.EngineEnv)), "engine-args")
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"flash", "nope"}, `no target "nope"`},
		{[]string{"flash", "--bogus"}, "flag provided but not defined"},
		{[]string{"debug", "app", "extra"}, "usage:"},
		{[]string{"attach", ""}, "usage:"},
	}
	for _, c := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(c.args, nil, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), c.want) {
			t.Fatalf("%v: exit %d stderr %q", c.args, code, stderr.String())
		}
	}
	if _, err := os.Stat(engineArgs); err == nil {
		t.Fatal("a refused command built")
	}
	// A failed build stops before the probe.
	t.Setenv("FAKE_EXIT", "3")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"flash"}, nil, &stdout, &stderr); code != 3 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(westLog); err == nil {
		t.Fatal("flashed after a failed build")
	}
}

// setEngineScript replaces containerProject's fake engine with script.
func setEngineScript(t *testing.T, script string) {
	t.Helper()
	if err := os.WriteFile(os.Getenv(container.EngineEnv), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestStoppedEngineExplainsItself(t *testing.T) {
	containerProject(t)
	setEngineScript(t, `echo cannot connect >&2; exit 1`)
	ensureVolume = func(container.Engine, string, string, string, int, int) error {
		return errors.New("volume create failed")
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"build"}, nil, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "colima start") {
		t.Fatalf("exit %d stderr %q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "volume create failed") {
		t.Fatalf("stderr %q", stderr.String())
	}
}

func TestVolumeErrorKeptWhenEngineRuns(t *testing.T) {
	containerProject(t)
	ensureVolume = func(container.Engine, string, string, string, int, int) error {
		return errors.New("volume create failed")
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"build"}, nil, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "volume create failed") {
		t.Fatalf("exit %d stderr %q", code, stderr.String())
	}
}

func TestSmallEngineWarnsOnInstallOnly(t *testing.T) {
	containerProject(t)
	setEngineScript(t, `echo 2 2147483648`)
	for args, want := range map[string]bool{"sync": true, "-v sync": true, "init": true, "west boards": false} {
		var stdout, stderr bytes.Buffer
		if code := run(strings.Fields(args), nil, &stdout, &stderr); code != 0 {
			t.Fatalf("%s: exit %d stderr %q", args, code, stderr.String())
		}
		if got := strings.Contains(stderr.String(), "warning: the container engine has 2 CPUs"); got != want {
			t.Fatalf("%s: warned %v, stderr %q", args, got, stderr.String())
		}
	}
}

func TestDoctorExitCode(t *testing.T) {
	containerProject(t)
	setEngineScript(t, `echo cannot connect >&2; exit 1`)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"doctor"}, nil, &stdout, &stderr); code != 1 {
		t.Fatalf("engine down: exit %d stderr %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "fail  running") || !strings.Contains(stdout.String(), "colima start") {
		t.Fatalf("stdout %q", stdout.String())
	}
	setEngineScript(t, `echo 4 8308363264`)
	stdout.Reset()
	if code := run([]string{"doctor"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("healthy: exit %d stdout %q", code, stdout.String())
	}
	if code := run([]string{"doctor", "extra"}, nil, &stdout, &stderr); code != 1 {
		t.Fatalf("extra argument: exit %d", code)
	}
}
