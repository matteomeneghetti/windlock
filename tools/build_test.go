package tools

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDTCMakeArgsSkipYAML(t *testing.T) {
	got := dtcMakeArgs(4, "/cache/build-tools/f/bin/flex", "/cache/build-tools/b/bin/bison")
	want := []string{
		"-j4",
		"dtc",
		"LEX=/cache/build-tools/f/bin/flex",
		"BISON=/cache/build-tools/b/bin/bison",
		"NO_YAML=1",
		"PKG_CONFIG=/usr/bin/false",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("make args %q, want %q", got, want)
	}
}

func TestBuildEnvKeepsNothingOfTheUsers(t *testing.T) {
	root := t.TempDir()
	for k, v := range map[string]string{
		"CC": "/opt/homebrew/bin/gcc-14", "CFLAGS": "-O3", "LDFLAGS": "-L/opt/homebrew/lib",
		"PKG_CONFIG_PATH": "/opt/homebrew/lib/pkgconfig", "TMPDIR": "/var/folders/x", "PATH": "/opt/homebrew/bin:/usr/bin",
		"DEVELOPER_DIR": "/Applications/Xcode.app/Contents/Developer",
	} {
		t.Setenv(k, v)
	}
	env := map[string]string{}
	for _, kv := range buildEnv(root, []string{"/root/cache/build-tools/k/bin"}) {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	for _, k := range []string{"CFLAGS", "LDFLAGS", "PKG_CONFIG_PATH"} {
		if _, ok := env[k]; ok {
			t.Errorf("%s leaked into the build", k)
		}
	}
	// On a Mac windlock names the developer tools' clang itself.
	if env["CC"] == "/opt/homebrew/bin/gcc-14" {
		t.Error("the user's CC leaked into the build")
	}
	path := strings.Split(env["PATH"], ":")
	if path[0] != "/root/cache/build-tools/k/bin" || strings.Join(path[len(path)-4:], ":") != "/usr/bin:/bin:/usr/sbin:/sbin" {
		t.Errorf("PATH %s", env["PATH"])
	}
	for _, dir := range path {
		if strings.HasPrefix(dir, "/opt/homebrew") {
			t.Errorf("PATH has the user's %s", dir)
		}
	}
	if !strings.HasPrefix(env["TMPDIR"], root) || !strings.HasPrefix(env["HOME"], root) {
		t.Errorf("TMPDIR %s HOME %s are outside the root", env["TMPDIR"], env["HOME"])
	}
	if env["DEVELOPER_DIR"] == "" || env["LC_ALL"] != "C" {
		t.Errorf("env %v", env)
	}
}

func TestExtractSourceFindsTopDirectory(t *testing.T) {
	root := t.TempDir()
	archive := tarGZ(t, map[string]string{"thing-2.1/configure": "#!/bin/sh\n"})
	src, err := extractSource(root, archive, strings.Repeat("a", 64))
	if err != nil || filepath.Base(src) != "thing-2.1" {
		t.Fatalf("src %s err %v", src, err)
	}
	flat := tarGZ(t, map[string]string{"a": "1", "b": "2"})
	if _, err := extractSource(root, flat, strings.Repeat("b", 64)); err == nil {
		t.Fatal("a tarball without one top directory was accepted")
	}
}
