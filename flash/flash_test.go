package flash

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/matteomeneghetti/windlock/internal/envvar"
	"github.com/matteomeneghetti/windlock/lockfile"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sysbuildProject lays out build/app like the app's sysbuild build.
func sysbuildProject(t *testing.T) (project, build string) {
	project = t.TempDir()
	build = filepath.Join(project, "build", "app")
	write(t, filepath.Join(build, "domains.yaml"), "default: app\nbuild_dir: "+build+"\ndomains:\n  - name: app\n    build_dir: "+build+"/app\n  - name: mcuboot\n    build_dir: "+build+"/mcuboot\nflash_order:\n  - mcuboot\n  - app\n")
	write(t, filepath.Join(build, "app", "zephyr", "runners.yaml"), "runners:\n- jlink\n- openocd\nflash-runner: jlink\ndebug-runner: openocd\nconfig:\n  gdb: /windlock/root/sdk/gdb\n  elf_file: zephyr.elf\n")
	return project, build
}

func tools(have ...string) Tools {
	return Tools{
		Bin:            "/r/host/id/bin",
		OpenOCDScripts: "/r/host/id/openocd/scripts",
		LookPath: func(name string) (string, error) {
			for _, h := range have {
				if h == name {
					return "/Applications/" + name, nil
				}
			}
			return "", errors.New("not found")
		},
	}
}

func TestArgsDefaultBuildAndRunner(t *testing.T) {
	project, build := sysbuildProject(t)
	// flash defaults to jlink, which is installed here: no overrides.
	args, runner, err := Args(Request{Project: project, DefaultBuild: build, Args: []string{"flash"}}, tools("JLinkExe"))
	if err != nil || runner != "jlink" {
		t.Fatalf("runner %s err %v", runner, err)
	}
	if want := []string{"flash", "-d", build, "--no-rebuild"}; !reflect.DeepEqual(args, want) {
		t.Fatalf("args %q", args)
	}
	// debugserver defaults to openocd, which gets the pinned binary and scripts.
	args, runner, err = Args(Request{Project: project, DefaultBuild: build, Args: []string{"debugserver"}}, tools())
	if err != nil || runner != "openocd" {
		t.Fatalf("runner %s err %v", runner, err)
	}
	if want := []string{"debugserver", "-d", build, "--no-rebuild", "--openocd", "/r/host/id/bin/openocd", "--openocd-search", "/r/host/id/openocd/scripts"}; !reflect.DeepEqual(args, want) {
		t.Fatalf("args %q", args)
	}
}

func TestArgsUserOptions(t *testing.T) {
	project, build := sysbuildProject(t)
	args, runner, err := Args(Request{Project: project, Args: []string{"flash", "-d", "build/app", "-r", "dfu-util", "--rebuild", "-f", "out.bin", "--", "-x"}}, tools())
	if err != nil || runner != "dfu-util" {
		t.Fatalf("runner %s err %v", runner, err)
	}
	want := []string{"flash", "-d", build, "-r", "dfu-util", "--rebuild", "-f", filepath.Join(project, "out.bin"), "--domain", "app", "--", "-x"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args\n got %q\nwant %q", args, want)
	}
	args, _, err = Args(Request{Project: project, Args: []string{"flash", "--build-dir=build/app", "--runner=openocd"}}, tools())
	if err != nil || args[1] != "--build-dir="+build || !strings.Contains(strings.Join(args, " "), "--openocd /r/host/id/bin/openocd") {
		t.Fatalf("args %q err %v", args, err)
	}
}

func TestArgsErrors(t *testing.T) {
	project, build := sysbuildProject(t)
	if _, _, err := Args(Request{Project: project, DefaultBuild: build, Args: []string{"flash"}}, tools()); err == nil ||
		!strings.Contains(err.Error(), "JLinkExe") || !strings.Contains(err.Error(), "segger.com") || !strings.Contains(err.Error(), "-r openocd") {
		t.Fatalf("missing J-Link err %v", err)
	}
	if _, _, err := Args(Request{Project: project, DefaultBuild: build, Args: []string{"flash", "-r", "stm32cubeprogrammer"}}, tools()); err == nil || !strings.Contains(err.Error(), "STM32_Programmer_CLI") {
		t.Fatalf("missing STM32Cube err %v", err)
	}
	if _, _, err := Args(Request{Project: project, Args: []string{"flash"}}, tools()); err == nil || !strings.Contains(err.Error(), "pass -d") {
		t.Fatalf("no build dir err %v", err)
	}
	if _, _, err := Args(Request{Project: project, Args: []string{"flash", "-d", "build/none"}}, tools()); err == nil || !strings.Contains(err.Error(), "windlock build") {
		t.Fatalf("unbuilt err %v", err)
	}
	if _, _, err := Args(Request{Project: project, Args: []string{"build"}}, tools()); err == nil {
		t.Fatal("west build accepted on the host")
	}
}

func TestEnsureBuildsOnceAndOnPinChange(t *testing.T) {
	root := t.TempDir()
	entry := lockfile.Zephyr{Name: "4.4.2", Commit: strings.Repeat("a", 40), PythonInputsSHA256: "in", PythonRequirements: []string{"west==1.5.0"}}
	copies, syncs := 0, 0
	steps := Steps{
		CopyZephyr: func(dst string) error {
			copies++
			write(t, filepath.Join(dst, "scripts", "west-commands.yml"), "west-commands: []\n")
			return nil
		},
		SyncVenv: func(venv string, pins []string) error {
			syncs++
			if !reflect.DeepEqual(pins, entry.PythonRequirements) {
				t.Fatalf("pins %v", pins)
			}
			return nil
		},
	}
	w, err := Ensure(root, entry, steps, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	if w.Dir != filepath.Join(root, "flash", entry.Commit) {
		t.Fatalf("dir %s", w.Dir)
	}
	for file, want := range map[string]string{
		filepath.Join(w.Dir, "zephyr", "west.yml"): manifest,
		filepath.Join(w.Dir, ".west", "config"):    westConfig,
	} {
		if raw, _ := os.ReadFile(file); string(raw) != want {
			t.Fatalf("%s: %q", file, raw)
		}
	}
	if _, err := Ensure(root, entry, steps, os.Stderr); err != nil || copies != 1 || syncs != 1 {
		t.Fatalf("second Ensure rebuilt: copies %d syncs %d err %v", copies, syncs, err)
	}
	entry.PythonRequirements = []string{"west==1.6.0"}
	if _, err := Ensure(root, entry, steps, os.Stderr); err != nil || copies != 2 || syncs != 2 {
		t.Fatalf("new pins kept the old workspace: copies %d syncs %d err %v", copies, syncs, err)
	}
	if _, err := Ensure(root, lockfile.Zephyr{Name: "x"}, steps, os.Stderr); err == nil {
		t.Fatal("an entry without pins was accepted")
	}
}

func TestEnvKeepsHomeInRoot(t *testing.T) {
	w := Workspace{Dir: "/r/flash/abc"}
	env := Env(w, "/r", tools(), []string{"PATH=/usr/bin", "HOME=/Users/me", "ZEPHYR_SDK_INSTALL_DIR=/x"})
	for k, want := range map[string]string{
		"HOME":            "/r/flash/home",
		"XDG_CACHE_HOME":  "/r/flash/home/.cache",
		"XDG_CONFIG_HOME": "/r/flash/home/.config",
		"ZEPHYR_BASE":     "/r/flash/abc/zephyr",
		"PATH":            "/r/host/id/bin:/r/flash/abc/.venv/bin:/usr/bin",
	} {
		if got := envvar.Get(env, k); got != want {
			t.Fatalf("%s=%q, want %q", k, got, want)
		}
	}
	if envvar.Get(env, "ZEPHYR_SDK_INSTALL_DIR") != "" {
		t.Fatal("ZEPHYR_SDK_INSTALL_DIR kept")
	}
}

func TestGDBArgs(t *testing.T) {
	_, build := sysbuildProject(t)
	domain, err := DomainDir(build)
	if err != nil || domain != build+"/app" {
		t.Fatalf("domain %s err %v", domain, err)
	}
	gdb, args, err := GDBArgs(domain, "openocd", "host.docker.internal:3333", true)
	if err != nil || gdb != "/windlock/root/sdk/gdb" {
		t.Fatalf("gdb %s err %v", gdb, err)
	}
	want := []string{"-ex", "set remotetimeout 10", "-ex", "target extended-remote host.docker.internal:3333", "-ex", "monitor reset halt", "-ex", "load", filepath.Join(domain, "zephyr", "zephyr.elf")}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args %q", args)
	}
	_, args, _ = GDBArgs(domain, "jlink", "host.docker.internal:2331", false)
	if want := []string{"-ex", "set remotetimeout 10", "-ex", "target remote host.docker.internal:2331", filepath.Join(domain, "zephyr", "zephyr.elf")}; !reflect.DeepEqual(args, want) {
		t.Fatalf("attach args %q", args)
	}
	if _, _, err := GDBArgs(domain, "dfu-util", "x:1", true); err == nil {
		t.Fatal("dfu-util accepted as a debug runner")
	}
	if dir, err := DomainDir(t.TempDir()); err != nil || dir == "" {
		t.Fatalf("a build without sysbuild: %s %v", dir, err)
	}
}

func TestDFUSendsOnlyTheDefaultDomain(t *testing.T) {
	project, build := sysbuildProject(t)
	args, _, err := Args(Request{Project: project, DefaultBuild: build, Args: []string{"flash", "-r", "dfu-util"}}, tools())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(args, " "), "--domain app") {
		t.Fatalf("args %q", args)
	}
	// The user's own choice stands.
	args, _, _ = Args(Request{Project: project, DefaultBuild: build, Args: []string{"flash", "-r", "dfu-util", "--domain=mcuboot"}}, tools())
	if joined := strings.Join(args, " "); strings.Contains(joined, "--domain app") || !strings.Contains(joined, "--domain=mcuboot") {
		t.Fatalf("args %q", args)
	}
	// Other runners flash every domain, each at its own address.
	args, _, _ = Args(Request{Project: project, DefaultBuild: build, Args: []string{"flash", "-r", "openocd"}}, tools())
	if strings.Contains(strings.Join(args, " "), "--domain") {
		t.Fatalf("args %q", args)
	}
	// Without sysbuild there are no domains to choose from.
	plain := filepath.Join(project, "build", "plain")
	args, _, err = Args(Request{Project: project, DefaultBuild: plain, Args: []string{"flash", "-r", "dfu-util"}}, tools())
	if err != nil || strings.Contains(strings.Join(args, " "), "--domain") {
		t.Fatalf("args %q err %v", args, err)
	}
}
