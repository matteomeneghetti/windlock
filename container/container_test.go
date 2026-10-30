package container

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestArgvMountsAndRunsAsUser(t *testing.T) {
	s := Spec{
		Engine:   Engine{Path: "/usr/bin/docker"},
		Image:    "img@sha256:abc",
		Platform: "linux/arm64",
		Volume:   "windlock-linux-aarch64-1234",
		Project:  "/p/app",
		Binary:   "/r/container/bin/windlock-1",
		Args:     []string{"build", "app", "-p", "always"},
		TTY:      true,
		UID:      501, GID: 20,
		Env:            []string{"TERM=xterm"},
		ProjectEntries: 3,
	}
	want := []string{
		"run", "--rm", "-i", "-t", "--user", "501:20", "--platform", "linux/arm64",
		"-v", "windlock-linux-aarch64-1234:/windlock/root",
		"-v", "/p/app:/p/app", "-w", "/p/app",
		"-v", "/r/container/bin/windlock-1:/usr/local/bin/windlock:ro",
		"-e", "WINDLOCK_IN_CONTAINER=1",
		"-e", "WINDLOCK_ROOT=/windlock/root",
		"-e", "HOME=/windlock/root/home",
		"-e", "WINDLOCK_PROJECT_ENTRIES=3",
		"-e", "TERM=xterm",
		"--entrypoint", "/usr/local/bin/windlock", "img@sha256:abc",
		"build", "app", "-p", "always",
	}
	if got := s.Argv(); !reflect.DeepEqual(got, want) {
		t.Fatalf("argv\n got %q\nwant %q", got, want)
	}
}

func TestArgvPodmanAndWindows(t *testing.T) {
	s := Spec{
		Engine:  Engine{Path: "podman", Podman: true},
		Image:   "img",
		Volume:  "windlock-linux-x86_64-abcd",
		Project: `C:\src\blinky\`,
		Binary:  `C:\bin\windlock`,
		UID:     -1, GID: -1,
		Windows: true,
	}
	got := strings.Join(s.Argv(), " ")
	for _, want := range []string{
		"--userns=keep-id",
		"-v windlock-linux-x86_64-abcd:/windlock/root",
		`-v C:\src\blinky\:/work/blinky -w /work/blinky`,
		"WINDLOCK_ROOT=/windlock/root",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("argv %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "--user ") || strings.Contains(got, " -t ") {
		t.Fatalf("argv %q", got)
	}
}

func TestFindEngine(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"podman", "nerdctl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv(EngineEnv, "")
	e, err := FindEngine()
	if err != nil || filepath.Base(e.Path) != "podman" || !e.Podman {
		t.Fatalf("engine %+v err %v", e, err)
	}
	t.Setenv(EngineEnv, "nerdctl")
	if e, err := FindEngine(); err != nil || filepath.Base(e.Path) != "nerdctl" || e.Podman {
		t.Fatalf("engine %+v err %v", e, err)
	}
	t.Setenv(EngineEnv, "docker")
	if _, err := FindEngine(); err == nil || !strings.Contains(err.Error(), "not on PATH") {
		t.Fatalf("err %v", err)
	}
	t.Setenv(EngineEnv, "")
	t.Setenv("PATH", t.TempDir())
	if _, err := FindEngine(); err == nil || !strings.Contains(err.Error(), "colima") {
		t.Fatalf("err %v", err)
	}
}

func TestTriple(t *testing.T) {
	if got, _ := Triple("arm64"); got != "linux-aarch64" {
		t.Fatal(got)
	}
	if got, _ := Triple("amd64"); got != "linux-x86_64" {
		t.Fatal(got)
	}
	if _, err := Triple("386"); err == nil {
		t.Fatal("386 accepted")
	}
}

func TestCheckMounts(t *testing.T) {
	project := t.TempDir()
	t.Setenv(entriesEnv, "")
	if err := CheckMounts(project); err != nil {
		t.Fatalf("without a count: %v", err)
	}
	t.Setenv(entriesEnv, "0")
	if err := CheckMounts(project); err != nil {
		t.Fatalf("host sees an empty project too: %v", err)
	}
	t.Setenv(entriesEnv, "2")
	if err := CheckMounts(project); err == nil || !strings.Contains(err.Error(), "does not share it") {
		t.Fatalf("unshared project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project, "windlock.yml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckMounts(project); err != nil || CountEntries(project) != 1 {
		t.Fatal(err)
	}
}

func TestVolumeName(t *testing.T) {
	a := VolumeName("/Users/me/.local/share/windlock", "linux-aarch64")
	if !strings.HasPrefix(a, "windlock-linux-aarch64-") || len(a) != len("windlock-linux-aarch64-")+8 {
		t.Fatalf("name %s", a)
	}
	if a != VolumeName("/Users/me/.local/share/windlock/", "linux-aarch64") {
		t.Fatal("a trailing slash changed the volume")
	}
	if a == VolumeName("/Users/me/e2e/root", "linux-aarch64") || a == VolumeName("/Users/me/.local/share/windlock", "linux-x86_64") {
		t.Fatal("another root or triple shares the volume")
	}
}

func TestEnsureVolumeCreatesOnce(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	engine := filepath.Join(dir, "docker")
	// volume inspect fails until volume create has run.
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\n" +
		"if [ \"$1 $2\" = 'volume inspect' ]; then [ -f " + dir + "/made ]; exit $?; fi\n" +
		"if [ \"$1 $2\" = 'volume create' ]; then touch " + dir + "/made; fi\nexit 0\n"
	if err := os.WriteFile(engine, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e := Engine{Path: engine}
	for i := 0; i < 2; i++ {
		if err := EnsureVolume(e, "img", "linux/arm64", "vol", 501, 20); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	calls := string(raw)
	if strings.Count(calls, "volume create vol") != 1 || strings.Count(calls, "volume inspect vol") != 2 {
		t.Fatalf("calls %q", calls)
	}
	if !strings.Contains(calls, "run --rm --user 0:0 --platform linux/arm64 -v vol:/windlock/root --entrypoint /bin/sh img -c mkdir -p /windlock/root/home && chown -R 501:20 /windlock/root") {
		t.Fatalf("calls %q", calls)
	}
}

func fakeEngine(t *testing.T, script string) Engine {
	t.Helper()
	path := filepath.Join(t.TempDir(), "engine")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return Engine{Path: path}
}

func TestGetInfo(t *testing.T) {
	info, err := GetInfo(fakeEngine(t, `echo 4 8308363264`))
	if err != nil || info != (Info{CPUs: 4, Memory: 8308363264}) {
		t.Fatalf("info %+v err %v", info, err)
	}
	if _, err := GetInfo(fakeEngine(t, `echo surprise`)); err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("err %v", err)
	}
}

func TestEngineInfoFormatByEngine(t *testing.T) {
	e := fakeEngine(t, `echo "$3"`)
	if out, err := engineInfo(e); err != nil || !strings.Contains(out, ".NCPU") {
		t.Fatalf("docker format %q err %v", out, err)
	}
	e.Podman = true
	if out, err := engineInfo(e); err != nil || !strings.Contains(out, ".Host.CPUs") {
		t.Fatalf("podman format %q err %v", out, err)
	}
}

func TestCheckRunning(t *testing.T) {
	if err := CheckRunning(fakeEngine(t, `echo 4 8308363264`)); err != nil {
		t.Fatal(err)
	}
	err := CheckRunning(fakeEngine(t, `echo cannot connect >&2; exit 1`))
	if err == nil || !strings.Contains(err.Error(), "colima start") || !strings.Contains(err.Error(), "cannot connect") {
		t.Fatalf("err %v", err)
	}
}

func TestSizeWarning(t *testing.T) {
	for _, tc := range []struct {
		info Info
		warn bool
	}{
		{Info{CPUs: 4, Memory: 8308363264}, false}, // what Colima reports for 8 GB
		{Info{CPUs: 8, Memory: 16 << 30}, false},
		{Info{CPUs: 2, Memory: 8 << 30}, true},
		{Info{CPUs: 4, Memory: 2 << 30}, true},
	} {
		if got := tc.info.SizeWarning(); (got != "") != tc.warn {
			t.Fatalf("%+v: warning %q", tc.info, got)
		}
	}
	if got := (Info{CPUs: 2, Memory: 2 << 30}).SizeWarning(); !strings.Contains(got, "2 CPUs and 2.0 GiB") {
		t.Fatalf("warning %q", got)
	}
}
