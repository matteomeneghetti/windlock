package lockfile

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRoundTripFixture(t *testing.T) {
	want := expectedFixture()

	var buf bytes.Buffer
	if err := Write(&buf, want); err != nil {
		t.Fatal(err)
	}
	got, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("round trip mismatch\nwant %#v\ngot  %#v", want, got)
	}

	parsed, err := ReadFile("testdata/windlock.lock.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, parsed) {
		t.Fatalf("fixture parsed as %#v\nwant %#v", parsed, want)
	}
}

func TestWriteIsByteStable(t *testing.T) {
	var a, b bytes.Buffer
	if err := Write(&a, expectedFixture()); err != nil {
		t.Fatal(err)
	}
	// Maps built in another insertion order encode to the same bytes.
	lock := expectedFixture()
	lock.Platforms = map[string]Platform{
		"macos-aarch64": lock.Platforms["macos-aarch64"],
		"linux-x86_64":  lock.Platforms["linux-x86_64"],
	}
	if err := Write(&b, lock); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatalf("encodings differ\n%s\n---\n%s", a.String(), b.String())
	}
}

func TestReadRejectsEmptyAndUnknownFields(t *testing.T) {
	if _, err := Read(strings.NewReader("")); err == nil {
		t.Fatal("empty file was accepted")
	}
	if _, err := Read(strings.NewReader("schema: 3\nbogus: 1\n")); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("unknown field err %v", err)
	}
}

func TestReadFileNamesOtherSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), Filename)
	if err := os.WriteFile(path, []byte("schema: 2\ndefault: \"4.4.2\"\nzephyr: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadFile(path)
	if err == nil || !strings.Contains(err.Error(), "schema 2") || !strings.Contains(err.Error(), "windlock init") {
		t.Fatalf("schema 2 err %v", err)
	}
}

func TestCheckSchema(t *testing.T) {
	lock := expectedFixture()
	if err := lock.CheckSchema(); err != nil {
		t.Fatal(err)
	}
	lock.Schema = 2
	if err := lock.CheckSchema(); err == nil || !strings.Contains(err.Error(), "windlock init") {
		t.Fatalf("schema 2 err %v", err)
	}
}

func TestDefaultEntryPlatformSDKs(t *testing.T) {
	lock := expectedFixture()
	entry, err := lock.DefaultEntry()
	if err != nil || entry.Name != "4.4.2" {
		t.Fatalf("default entry %s err %v", entry.Name, err)
	}
	if got := lock.SDKs(); !reflect.DeepEqual(got, []string{"1.0.1", "0.17.4"}) {
		t.Fatalf("sdks %v", got)
	}
	if _, err := lock.Platform("linux-x86_64"); err != nil {
		t.Fatal(err)
	}
	_, err = lock.Platform("windows-x86_64")
	if err == nil || !strings.Contains(err.Error(), "linux-x86_64, macos-aarch64") {
		t.Fatalf("missing platform err %v", err)
	}
	if _, err := (Lockfile{}).DefaultEntry(); err == nil {
		t.Fatal("empty lockfile has a default entry")
	}
}

func TestWriteFileIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, Filename)
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, expectedFixture()); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(path)
	if err != nil || !reflect.DeepEqual(got, expectedFixture()) {
		t.Fatalf("read back %+v err %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temporary file left behind: %v", entries)
	}
	if err := WriteFile(filepath.Join(dir, "missing", Filename), expectedFixture()); err == nil {
		t.Fatal("write into a missing directory succeeded")
	}
}

func TestDiff(t *testing.T) {
	want := expectedFixture()
	if d, err := Diff(want, expectedFixture()); err != nil || d != nil {
		t.Fatalf("equal lockfiles diff %v err %v", d, err)
	}
	got := expectedFixture()
	got.Zephyr[0].Projects[0].Revision = "moved"
	got.Zephyr[1].Toolchains = append(got.Zephyr[1].Toolchains, "arm-zephyr-eabi")
	delete(got.Platforms, "macos-aarch64")
	d, err := Diff(want, got)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(d, "\n")
	for _, line := range []string{
		"zephyr[4.4.2].projects[cmsis].revision: want fixture-cmsis, got moved",
		"zephyr[4.3.1].toolchains[1]: want nothing, got arm-zephyr-eabi",
		"platforms.macos-aarch64.host_tools.cmake.sha256: want fixture-cmake-mac-sha256, got nothing",
	} {
		if !strings.Contains(text, line) {
			t.Fatalf("diff lacks %q:\n%s", line, text)
		}
	}
}

func TestConfigFixtureAndTargets(t *testing.T) {
	cfg, err := ReadConfigFile("testdata/windlock.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Zephyr, []string{"v4.4.2"}) {
		t.Fatalf("zephyr %v", cfg.Zephyr)
	}
	app, err := cfg.TargetNamed("")
	if err != nil || app.Name != "app" || app.Board != "acme_board" {
		t.Fatalf("default target %+v err %v", app, err)
	}
	sensor, err := cfg.TargetNamed("sensor")
	if err != nil || sensor.Source != "apps/sensor" {
		t.Fatalf("named target %+v err %v", sensor, err)
	}
	if _, err := cfg.TargetNamed("missing"); err == nil || !strings.Contains(err.Error(), ConfigFilename) {
		t.Fatalf("missing target err %v", err)
	}
	if got := cfg.Boards(); !reflect.DeepEqual(got, []string{"acme_board", "acme_sensor"}) {
		t.Fatalf("boards %v", got)
	}
	cfg.Target = ""
	if _, err := cfg.TargetNamed(""); err == nil || !strings.Contains(err.Error(), "target is not set") {
		t.Fatalf("unset target err %v", err)
	}
	if _, err := (Config{}).TargetNamed("app"); err == nil || !strings.Contains(err.Error(), "no targets") {
		t.Fatalf("no targets err %v", err)
	}
}

func TestConfigTargetEdits(t *testing.T) {
	var cfg Config
	if err := cfg.AddTarget("app", Target{Board: "acme_board", Source: "apps/app"}); err != nil {
		t.Fatal(err)
	}
	if cfg.Target != "app" {
		t.Fatalf("first target is not the default: %q", cfg.Target)
	}
	if err := cfg.AddTarget("sensor", Target{Board: "acme_sensor", Source: "apps/sensor"}); err != nil {
		t.Fatal(err)
	}
	if cfg.Target != "app" {
		t.Fatalf("second target took the default: %q", cfg.Target)
	}
	for name, want := range map[string]string{"app": "already has", "a/b": "letters", "..": "letters", "": "letters"} {
		if err := cfg.AddTarget(name, Target{Board: "b", Source: "s"}); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("add %q err %v", name, err)
		}
	}
	if err := cfg.AddTarget("x", Target{Board: "b"}); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("no source err %v", err)
	}
	if err := cfg.SetDefaultTarget("sensor"); err != nil || cfg.Target != "sensor" {
		t.Fatalf("default %q err %v", cfg.Target, err)
	}
	if err := cfg.SetDefaultTarget("nope"); err == nil {
		t.Fatal("unknown default was accepted")
	}
	if err := cfg.RemoveTarget("sensor"); err != nil || cfg.Target != "" || len(cfg.Targets) != 1 {
		t.Fatalf("after removing the default: %+v err %v", cfg, err)
	}
	if err := cfg.RemoveTarget("sensor"); err == nil {
		t.Fatal("removed a missing target")
	}
	if err := cfg.RemoveTarget("app"); err != nil || cfg.Targets != nil {
		t.Fatalf("after removing the last: %+v err %v", cfg, err)
	}
}

func TestConfigZephyrEdits(t *testing.T) {
	cfg := Config{Zephyr: []string{"v4.4.2", "v4.3.1", "main"}}
	if err := cfg.MoveZephyrFirst(2); err != nil || !reflect.DeepEqual(cfg.Zephyr, []string{"main", "v4.4.2", "v4.3.1"}) {
		t.Fatalf("move %v err %v", cfg.Zephyr, err)
	}
	if err := cfg.RemoveZephyr(1); err != nil || !reflect.DeepEqual(cfg.Zephyr, []string{"main", "v4.3.1"}) {
		t.Fatalf("remove %v err %v", cfg.Zephyr, err)
	}
	if err := cfg.RemoveZephyr(5); err == nil {
		t.Fatal("removed out of range")
	}
	_ = cfg.RemoveZephyr(0)
	if err := cfg.RemoveZephyr(0); err == nil || !strings.Contains(err.Error(), "only zephyr v4.3.1") {
		t.Fatalf("remove last err %v", err)
	}

	lock := expectedFixture()
	lock.MoveZephyrFirst(1)
	if lock.Zephyr[0].Name != "4.3.1" || lock.Zephyr[1].Name != "4.4.2" {
		t.Fatalf("lock order %s %s", lock.Zephyr[0].Name, lock.Zephyr[1].Name)
	}
	lock.RemoveZephyr(0)
	if len(lock.Zephyr) != 1 || lock.Zephyr[0].Name != "4.4.2" {
		t.Fatalf("lock after remove %+v", lock.Zephyr)
	}
	if got := expectedFixture(); got.Zephyr[0].Name != "4.4.2" {
		t.Fatal("editing a copy changed the fixture")
	}
}

func TestWriteConfigFileRoundTrip(t *testing.T) {
	cfg, err := ReadConfigFile("testdata/windlock.yml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), ConfigFilename)
	if err := WriteConfigFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	back, err := ReadConfigFile(path)
	if err != nil || !reflect.DeepEqual(back, cfg) {
		t.Fatalf("round trip %+v err %v", back, err)
	}
	text, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(text), "# Managed by windlock") {
		t.Fatalf("header:\n%s", text)
	}
}

func expectedFixture() Lockfile {
	const url = "https://github.com/zephyrproject-rtos/zephyr"
	bundle := func(version, triple string) Bundle {
		return Bundle{
			Filename: "zephyr-sdk-" + version + "_" + triple + "_minimal.tar.xz",
			URL:      "fixture-url-" + version + "-" + triple,
			SHA256:   "fixture-sha-" + version + "-" + triple,
		}
	}
	return Lockfile{
		Schema:    3,
		West:      "fixture-west",
		Container: Container{Image: "fixture-image@sha256:fixture"},
		HostTools: map[string]string{"cmake": "fixture-cmake", "python": "fixture-python"},
		Zephyr: []Zephyr{
			{
				Name:           "4.4.2",
				URL:            url,
				Revision:       "v4.4.2",
				Commit:         "0123456789abcdef0123456789abcdef01234567",
				SDK:            "1.0.1",
				Toolchains:     []string{"x86_64-zephyr-elf"},
				ManifestSHA256: "fixture-freeze-4.4.2",
				Projects:       []Project{{Name: "cmsis", Path: "modules/hal/cmsis", Revision: "fixture-cmsis"}},

				PythonInputsSHA256: "fixture-python-inputs-4.4.2",
				PythonRequirements: []string{"pyelftools==0.32", "west==1.5.0", "windows-curses==2.4.1 ; sys_platform == 'win32'"},
			},
			{
				Name:           "4.3.1",
				URL:            url,
				Revision:       "v4.3.1",
				Commit:         "fedcba9876543210fedcba9876543210fedcba98",
				SDK:            "0.17.4",
				Toolchains:     []string{"x86_64-zephyr-elf"},
				ManifestSHA256: "fixture-freeze-4.3.1",
				Projects:       []Project{},

				PythonInputsSHA256: "fixture-python-inputs-4.3.1",
				PythonRequirements: []string{"west==1.5.0"},
			},
		},
		Platforms: map[string]Platform{
			"linux-x86_64": {
				HostTools: map[string]Archive{
					"cmake":  {URL: "fixture-cmake-linux", SHA256: "fixture-cmake-linux-sha256"},
					"python": {URL: "fixture-python-linux", SHA256: "fixture-python-linux-sha256"},
				},
				SDK: map[string]Bundle{"1.0.1": bundle("1.0.1", "linux-x86_64")},
			},
			"macos-aarch64": {
				HostTools: map[string]Archive{
					"cmake": {URL: "fixture-cmake-mac", SHA256: "fixture-cmake-mac-sha256"},
				},
				SDK: map[string]Bundle{"0.17.4": bundle("0.17.4", "macos-aarch64")},
			},
		},
	}
}
