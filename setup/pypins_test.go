package setup

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// requirementTree writes Zephyr-style requirement files under ws and returns
// the top file west packages pip would list.
func requirementTree(t *testing.T, ws, base string) string {
	top := filepath.Join(ws, "zephyr", "scripts", "requirements.txt")
	writeFile(t, top, "-r requirements-base.txt\n--requirement=requirements-build-test.txt\n")
	writeFile(t, filepath.Join(ws, "zephyr", "scripts", "requirements-base.txt"), base)
	writeFile(t, filepath.Join(ws, "zephyr", "scripts", "requirements-build-test.txt"), "-cconstraints.txt\npytest\n")
	writeFile(t, filepath.Join(ws, "zephyr", "scripts", "constraints.txt"), "pytest<9\n")
	return top
}

func TestRequirementsSHA256FollowsIncludesAndIgnoresLocation(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	sumA, err := requirementsSHA256([]string{requirementTree(t, a, "pyelftools>=0.29\n")}, a, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sumB, err := requirementsSHA256([]string{requirementTree(t, b, "pyelftools>=0.29\n")}, b, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if sumA != sumB || !isHex(sumA, 64) {
		t.Fatalf("same files in two workspaces hash to %s and %s", sumA, sumB)
	}
	// Each included file counts, including the -c one.
	writeFile(t, filepath.Join(b, "zephyr", "scripts", "constraints.txt"), "pytest<10\n")
	sumC, err := requirementsSHA256([]string{filepath.Join(b, "zephyr", "scripts", "requirements.txt")}, b, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if sumC == sumA {
		t.Fatal("a changed constraints file kept the hash")
	}
}

func TestRequirementsSHA256NamesProjectFiles(t *testing.T) {
	ws, app := t.TempDir(), t.TempDir()
	req := filepath.Join(app, "requirements.txt")
	writeFile(t, req, "intelhex\n")
	if _, err := requirementsSHA256([]string{req}, ws, app); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "requirements.txt")
	writeFile(t, outside, "intelhex\n")
	if _, err := requirementsSHA256([]string{outside}, ws, app); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("err %v", err)
	}
}

func TestIncludedFiles(t *testing.T) {
	got := includedFiles([]byte("# comment\n-r a.txt\n-rb.txt\n--requirement c.txt\n--constraint=d.txt\n-c e.txt\npyyaml>=6\n-e .\n--index-url x\n"))
	if want := []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("included %v", got)
	}
}

func TestParseRequirementFlags(t *testing.T) {
	got, err := parseRequirementFlags("-r /ws/zephyr/scripts/requirements.txt\n-r /ws/modules/x/requirements.txt\n")
	if err != nil || !reflect.DeepEqual(got, []string{"/ws/zephyr/scripts/requirements.txt", "/ws/modules/x/requirements.txt"}) {
		t.Fatalf("files %v err %v", got, err)
	}
	if _, err := parseRequirementFlags("ERROR: no venv\n"); err == nil {
		t.Fatal("unexpected line accepted")
	}
	if _, err := parseRequirementFlags("\n"); err == nil {
		t.Fatal("empty list accepted")
	}
}

func TestParsePins(t *testing.T) {
	got, err := parsePins("# header\npyelftools==0.32\n\nwindows-curses==2.4.1 ; sys_platform == 'win32'\n")
	if err != nil || !reflect.DeepEqual(got, []string{"pyelftools==0.32", "windows-curses==2.4.1 ; sys_platform == 'win32'"}) {
		t.Fatalf("pins %v err %v", got, err)
	}
	if _, err := parsePins("pyyaml>=6\n"); err == nil {
		t.Fatal("a range was accepted as a pin")
	}
}

func TestCheckFrozen(t *testing.T) {
	pins := []string{"PyYAML==6.0.2", "python-dotenv==1.1.0", "windows-curses==2.4.1 ; sys_platform == 'win32'"}
	if err := checkFrozen("pyyaml==6.0.2\npython_dotenv==1.1.0\n", pins); err != nil {
		t.Fatal(err)
	}
	if err := checkFrozen("pyyaml==6.0.3\n", pins); err == nil || !strings.Contains(err.Error(), "pyyaml==6.0.3") {
		t.Fatalf("err %v", err)
	}
	if err := checkFrozen("pip==25.0\nsetuptools==80.0\n", pins); err != nil {
		t.Fatalf("seed packages: %v", err)
	}
	if err := checkFrozen("setuptools==79.0\n", append(pins, "setuptools==80.0")); err == nil {
		t.Fatal("a pinned seed package at another version was accepted")
	}
	if err := checkFrozen("intelhex==2.3.0\n", pins); err == nil {
		t.Fatal("an unpinned package was accepted")
	}
}

// fakePythonTools writes a west that lists req for packages pip, and a uv that
// records its argv, compiles to compiled and freezes to frozen.
func fakePythonTools(t *testing.T, req, compiled, frozen string) (hostEnv, workspace, string) {
	t.Helper()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "uv-args")
	west := filepath.Join(dir, "west")
	writeFile(t, west, "#!/bin/sh\n[ \"$1 $2\" = 'packages pip' ] && echo '-r "+req+"'\nexit 0\n")
	writeFile(t, filepath.Join(dir, "compiled"), compiled)
	writeFile(t, filepath.Join(dir, "frozen"), frozen)
	uv := filepath.Join(dir, "bin", "uv")
	writeFile(t, uv, "#!/bin/sh\necho \"$*\" >> \""+argsFile+"\"\n"+
		"case \"$2\" in compile) cat \""+filepath.Join(dir, "compiled")+"\";; freeze) cat \""+filepath.Join(dir, "frozen")+"\";; esac\nexit 0\n")
	for _, p := range []string{west, uv} {
		if err := os.Chmod(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h := hostEnv{root: dir, triple: "linux-x86_64", hostBin: filepath.Join(dir, "bin"), log: io.Discard}
	return h, workspace{dir: filepath.Dir(filepath.Dir(filepath.Dir(req))), venv: filepath.Join(dir, "venv"), west: west}, argsFile
}

func TestPythonPinsReuseResolveAndRefuse(t *testing.T) {
	ws := t.TempDir()
	req := requirementTree(t, ws, "pyelftools>=0.29\n")
	h, w, argsFile := fakePythonTools(t, req, "pyelftools==0.32\nwest==1.5.0\n", "")
	inputs, err := requirementsSHA256([]string{req}, ws, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	locked := []string{"pyelftools==0.31", "west==1.5.0"}

	// Unchanged inputs keep the locked pins, even where a resolve is allowed.
	got, pins, err := pythonPins(h, w, t.TempDir(), entrySpec{name: "4.4.2", pythonInputs: inputs, pythonPins: locked, resolvePython: true}, "1.5.0")
	if err != nil || got != inputs || !reflect.DeepEqual(pins, locked) {
		t.Fatalf("inputs %s pins %v err %v", got, pins, err)
	}
	if _, err := os.Stat(argsFile); err == nil {
		t.Fatal("uv ran although the pins were reused")
	}

	// Changed inputs: init and add resolve again, with west pinned.
	_, pins, err = pythonPins(h, w, t.TempDir(), entrySpec{name: "4.4.2", pythonInputs: strings.Repeat("0", 64), pythonPins: locked, resolvePython: true}, "1.5.0")
	if err != nil || !reflect.DeepEqual(pins, []string{"pyelftools==0.32", "west==1.5.0"}) {
		t.Fatalf("pins %v err %v", pins, err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "pip compile --universal") || !strings.Contains(string(args), req) {
		t.Fatalf("uv args %q", args)
	}

	// sync never resolves.
	_, _, err = pythonPins(h, w, t.TempDir(), entrySpec{name: "4.4.2", pythonInputs: strings.Repeat("0", 64), pythonPins: locked}, "1.5.0")
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("err %v", err)
	}
	_, _, err = pythonPins(h, w, t.TempDir(), entrySpec{name: "4.4.2"}, "1.5.0")
	if err == nil || !strings.Contains(err.Error(), "rerun windlock init") {
		t.Fatalf("err %v", err)
	}
}

func TestSyncPinsRejectsUnpinnedPackage(t *testing.T) {
	ws := t.TempDir()
	req := requirementTree(t, ws, "pyelftools\n")
	h, w, argsFile := fakePythonTools(t, req, "", "pyelftools==0.32\n")
	if err := syncPins(h, w, []string{"pyelftools==0.32"}); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pip sync --python", "pip check --python", "pip freeze --python"} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("uv args %q lack %q", args, want)
		}
	}
	h, w, _ = fakePythonTools(t, req, "", "pyelftools==0.32\nintelhex==2.3.0\n")
	if err := syncPins(h, w, []string{"pyelftools==0.32"}); err == nil || !strings.Contains(err.Error(), "intelhex") {
		t.Fatalf("err %v", err)
	}
}

func TestUVEnvStaysInRoot(t *testing.T) {
	t.Setenv("UV_INDEX_URL", "https://example.invalid/simple")
	env := uvEnv(os.Environ(), "/r")
	for _, want := range []string{"UV_CACHE_DIR=/r/cache/uv", "UV_PYTHON_DOWNLOADS=never", "UV_NO_CONFIG=1"} {
		if !contains(env, want) {
			t.Fatalf("env lacks %s", want)
		}
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, "UV_INDEX_URL=") {
			t.Fatalf("env keeps %s", kv)
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
