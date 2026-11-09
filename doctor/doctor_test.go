package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matteomeneghetti/windlock/container"
	"github.com/matteomeneghetti/windlock/lockfile"
)

// The fake engine answers from $INFO_EXIT, $IMAGE_EXIT, $VOLUME_EXIT and $RUN_OUT.
const fakeEngine = `#!/bin/sh
case "$1" in
info) [ "${INFO_EXIT:-0}" = 0 ] || { echo down >&2; exit 1; }; echo "${INFO:-4 8308363264}" ;;
image) exit "${IMAGE_EXIT:-0}" ;;
volume) exit "${VOLUME_EXIT:-0}" ;;
run) printf '%b\n' "${RUN_OUT:-workspace\nsdk\nentries 3}" ;;
esac
`

func testConfig(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	engine := filepath.Join(dir, "engine")
	if err := os.WriteFile(engine, []byte(fakeEngine), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(container.EngineEnv, engine)
	project := filepath.Join(dir, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	return Config{
		Project: project, Root: filepath.Join(dir, "no", "such", "root"), GOOS: "linux", GOARCH: "arm64",
		LookPath:  func(name string) (string, error) { return "/usr/bin/" + name, nil },
		FreeBytes: func(string) (uint64, error) { return 100 << 30, nil },
	}
}

func writeProject(t *testing.T, cfg Config, image string) {
	t.Helper()
	conf := "target: app\ntargets:\n  app:\n    board: acme_board\n    source: apps/app\n"
	if err := os.WriteFile(filepath.Join(cfg.Project, lockfile.ConfigFilename), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	lock := "schema: 1\nwest: 1.5.0\ncontainer:\n  image: " + image + "\nzephyr:\n  - name: \"4.4.1\"\n    commit: abc\n    sdk: 1.0.1\n"
	if err := os.WriteFile(filepath.Join(cfg.Project, lockfile.Filename), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
}

func find(t *testing.T, results []Result, name string) Result {
	t.Helper()
	for _, r := range results {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no %s check in %+v", name, results)
	return Result{}
}

func TestHealthyProject(t *testing.T) {
	cfg := testConfig(t)
	writeProject(t, cfg, "img@sha256:1")
	results := Run(cfg)
	if Failed(results) {
		t.Fatalf("%+v", results)
	}
	for name, want := range map[string]Status{"engine": OK, "running": OK, "vm size": OK, "disk": Skip, "compiler": OK, "lockfile": OK, "image": OK, "install": OK, "project": OK, "flash": Skip} {
		if got := find(t, results, name).Status; got != want {
			t.Fatalf("%s: %s, want %s: %+v", name, got, want, results)
		}
	}
}

func TestNoEngine(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv(container.EngineEnv, filepath.Join(t.TempDir(), "missing"))
	results := Run(cfg)
	if !Failed(results) || find(t, results, "engine").Status != Fail || find(t, results, "running").Status != Skip {
		t.Fatalf("%+v", results)
	}
}

func TestEngineDownSkipsWhatNeedsIt(t *testing.T) {
	cfg := testConfig(t)
	writeProject(t, cfg, "img@sha256:1")
	t.Setenv("INFO_EXIT", "1")
	results := Run(cfg)
	if r := find(t, results, "running"); r.Status != Fail || !strings.Contains(r.Fix, "colima start") {
		t.Fatalf("%+v", r)
	}
	if find(t, results, "vm size").Status != Skip || find(t, results, "install").Status != Skip {
		t.Fatalf("%+v", results)
	}
}

func TestSmallVMWarnsOnly(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("INFO", "2 2147483648")
	results := Run(cfg)
	if r := find(t, results, "vm size"); r.Status != Warn || !strings.Contains(r.Detail, "2 CPUs, 2.0 GiB") {
		t.Fatalf("%+v", r)
	}
	if Failed(results) {
		t.Fatalf("a warning failed: %+v", results)
	}
}

func TestDiskOnlyMattersBeforeInstall(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("VOLUME_EXIT", "1")
	cfg.FreeBytes = func(path string) (uint64, error) {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("checked a path that does not exist: %s", path)
		}
		return 5 << 30, nil
	}
	if r := find(t, Run(cfg), "disk"); r.Status != Warn || !strings.Contains(r.Detail, "5 GB") {
		t.Fatalf("%+v", r)
	}
	cfg.FreeBytes = func(string) (uint64, error) { return 0, errors.New("no statfs") }
	if r := find(t, Run(cfg), "disk"); r.Status != Skip {
		t.Fatalf("%+v", r)
	}
}

func TestMissingCompilerWarns(t *testing.T) {
	cfg := testConfig(t)
	cfg.LookPath = func(name string) (string, error) {
		if name == "make" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	if r := find(t, Run(cfg), "compiler"); r.Status != Warn || !strings.Contains(r.Detail, "make") {
		t.Fatalf("%+v", r)
	}
}

func TestOutsideAProject(t *testing.T) {
	results := Run(testConfig(t))
	if r := find(t, results, "project"); r.Status != Skip {
		t.Fatalf("%+v", r)
	}
	if Failed(results) {
		t.Fatalf("%+v", results)
	}
}

func TestLockfileProblems(t *testing.T) {
	cfg := testConfig(t)
	conf := filepath.Join(cfg.Project, lockfile.ConfigFilename)
	if err := os.WriteFile(conf, []byte("target: app\ntargets:\n  app:\n    board: b\n    source: s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := find(t, Run(cfg), "lockfile"); r.Status != Fail || !strings.Contains(r.Fix, "windlock init") {
		t.Fatalf("no lockfile: %+v", r)
	}
	writeProject(t, cfg, `""`)
	if r := find(t, Run(cfg), "lockfile"); r.Status != Fail || !strings.Contains(r.Detail, "no container image") {
		t.Fatalf("no image pin: %+v", r)
	}
}

func TestImageAndVolumeMissing(t *testing.T) {
	cfg := testConfig(t)
	writeProject(t, cfg, "img@sha256:1")
	t.Setenv("VOLUME_EXIT", "1")
	if r := find(t, Run(cfg), "install"); r.Status != Warn || !strings.Contains(r.Fix, "windlock sync") {
		t.Fatalf("no volume: %+v", r)
	}
	t.Setenv("IMAGE_EXIT", "1")
	results := Run(cfg)
	if find(t, results, "image").Status != Warn || find(t, results, "install").Status != Skip || Failed(results) {
		t.Fatalf("no image: %+v", results)
	}
}

func TestInstallIncompleteAndProjectNotShared(t *testing.T) {
	cfg := testConfig(t)
	writeProject(t, cfg, "img@sha256:1")
	t.Setenv("RUN_OUT", `workspace\nentries 0`)
	results := Run(cfg)
	if r := find(t, results, "install"); r.Status != Warn {
		t.Fatalf("%+v", r)
	}
	if r := find(t, results, "project"); r.Status != Fail || !strings.Contains(r.Fix, "colima") {
		t.Fatalf("%+v", r)
	}
	if !Failed(results) {
		t.Fatal("an unshared project did not fail")
	}
}

func TestVendorRunner(t *testing.T) {
	cfg := testConfig(t)
	writeProject(t, cfg, "img@sha256:1")
	runners := filepath.Join(cfg.Project, "build", "app", "zephyr", "runners.yaml")
	if err := os.MkdirAll(filepath.Dir(runners), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runners, []byte("flash-runner: jlink\ndebug-runner: jlink\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := find(t, Run(cfg), "flash"); r.Status != OK || !strings.Contains(r.Detail, "jlink") {
		t.Fatalf("installed: %+v", r)
	}
	cfg.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	if r := find(t, Run(cfg), "flash"); r.Status != Warn || !strings.Contains(r.Fix, "segger") {
		t.Fatalf("missing: %+v", r)
	}
}
