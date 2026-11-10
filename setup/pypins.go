package setup

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/matteomeneghetti/windlock/internal/envvar"
)

// uvEnv keeps uv's cache under the root and stops it reading user config or
// downloading a Python of its own.
func uvEnv(env []string, root string) []string {
	env = envvar.Set(env, "UV_CACHE_DIR", filepath.Join(root, "cache", "uv"))
	env = envvar.Set(env, "UV_PYTHON_DOWNLOADS", "never")
	env = envvar.Set(env, "UV_NO_CONFIG", "1")
	return envvar.Unset(env, "UV_INDEX_URL")
}

func (h hostEnv) uv() string { return filepath.Join(h.hostBin, "uv") }

func (h hostEnv) uvRun(w workspace, args ...string) (string, error) {
	return runCapture(h.log, w.dir, uvEnv(h.env(w), h.root), h.uv(), args...)
}

// pythonPins returns the pins for w: the locked ones while the requirement
// files are unchanged, else a new resolution when spec allows it.
func pythonPins(h hostEnv, w workspace, appDir string, spec entrySpec, westVersion string) (inputs string, pins []string, err error) {
	text, err := runCapture(io.Discard, w.dir, h.env(w), w.west, "packages", "pip")
	if err != nil {
		return "", nil, fmt.Errorf("west packages pip: %w", err)
	}
	files, err := parseRequirementFlags(text)
	if err != nil {
		return "", nil, err
	}
	inputs, err = requirementsSHA256(files, w.dir, appDir)
	if err != nil {
		return "", nil, err
	}
	if inputs == spec.pythonInputs && len(spec.pythonPins) > 0 {
		return inputs, spec.pythonPins, nil
	}
	if !spec.resolvePython {
		if spec.pythonInputs == "" {
			return "", nil, fmt.Errorf("zephyr %s has no python_requirements in the lockfile; rerun windlock init", spec.name)
		}
		return "", nil, fmt.Errorf("zephyr %s: the Python requirement files changed since the lockfile was written; rerun windlock init", spec.name)
	}
	fmt.Fprintf(h.log, "%s: resolving Python packages\n", spec.name)
	pins, err = resolvePins(h, w, files, westVersion)
	return inputs, pins, err
}

// parseRequirementFlags reads the "-r <file>" lines west packages pip prints.
func parseRequirementFlags(text string) ([]string, error) {
	var files []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		file, ok := strings.CutPrefix(line, "-r ")
		if !ok {
			return nil, fmt.Errorf("west packages pip: unexpected line %q", line)
		}
		files = append(files, strings.TrimSpace(file))
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("west packages pip listed no requirement files")
	}
	return files, nil
}

// requirementsSHA256 hashes each file and those it includes with -r or -c,
// named relative to the workspace or the project, so every host agrees.
func requirementsSHA256(files []string, ws, appDir string) (string, error) {
	contents := map[string][]byte{}
	var visit func(path string) error
	visit = func(path string) error {
		name, err := portableName(path, ws, appDir)
		if err != nil {
			return err
		}
		if _, ok := contents[name]; ok {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		contents[name] = raw
		for _, inc := range includedFiles(raw) {
			if !filepath.IsAbs(inc) {
				inc = filepath.Join(filepath.Dir(path), inc)
			}
			if err := visit(inc); err != nil {
				return err
			}
		}
		return nil
	}
	for _, f := range files {
		if err := visit(f); err != nil {
			return "", err
		}
	}
	names := make([]string, 0, len(contents))
	for name := range contents {
		names = append(names, name)
	}
	sort.Strings(names)
	sum := sha256.New()
	for _, name := range names {
		fileSum := sha256.Sum256(contents[name])
		fmt.Fprintf(sum, "%s %s\n", name, hex.EncodeToString(fileSum[:]))
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func portableName(path, ws, appDir string) (string, error) {
	for _, base := range []struct{ prefix, dir string }{{"workspace", ws}, {"project", appDir}} {
		rel, err := filepath.Rel(base.dir, filepath.Clean(path))
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return base.prefix + "/" + filepath.ToSlash(rel), nil
		}
	}
	return "", fmt.Errorf("requirement file %s is outside the workspace and the project", path)
}

// includedFiles returns the files a requirements file names with -r, -c,
// --requirement or --constraint.
func includedFiles(raw []byte) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		first := fields[0]
		switch {
		case first == "-r" || first == "-c" || first == "--requirement" || first == "--constraint":
			if len(fields) > 1 {
				out = append(out, fields[1])
			}
		case strings.HasPrefix(first, "--requirement=") || strings.HasPrefix(first, "--constraint="):
			_, file, _ := strings.Cut(first, "=")
			out = append(out, file)
		case len(first) > 2 && (strings.HasPrefix(first, "-r") || strings.HasPrefix(first, "-c")) && first[2] != '-':
			out = append(out, first[2:])
		}
	}
	return out
}

// resolvePins resolves files plus west==westVersion into one set of pins
// valid on every platform, with markers where a package is platform-only.
func resolvePins(h hostEnv, w workspace, files []string, westVersion string) ([]string, error) {
	westFile, err := writeTemp(h.root, "west-*.txt", "west=="+westVersion+"\n")
	if err != nil {
		return nil, err
	}
	defer os.Remove(westFile)
	args := []string{"pip", "compile", "--universal", "--python", filepath.Join(w.venv, "bin", "python"),
		"--no-header", "--no-annotate", "--quiet", westFile}
	args = append(args, files...)
	text, err := h.uvRun(w, args...)
	if err != nil {
		return nil, fmt.Errorf("uv pip compile: %w", err)
	}
	return parsePins(text)
}

// parsePins keeps the requirement lines of uv pip compile output.
func parsePins(text string) ([]string, error) {
	var pins []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "==") {
			return nil, fmt.Errorf("uv pip compile: %q is not pinned to one version", line)
		}
		pins = append(pins, line)
	}
	if len(pins) == 0 {
		return nil, fmt.Errorf("uv pip compile resolved no packages")
	}
	return pins, nil
}

// syncPins makes the venv hold exactly pins, then checks their dependencies
// and that nothing else is installed.
func syncPins(h hostEnv, w workspace, pins []string) error {
	file, err := writeTemp(h.root, "pins-*.txt", strings.Join(pins, "\n")+"\n")
	if err != nil {
		return err
	}
	defer os.Remove(file)
	python := filepath.Join(w.venv, "bin", "python")
	fmt.Fprintf(h.log, "uv pip sync (%d packages)\n", len(pins))
	if _, err := h.uvRun(w, "pip", "sync", "--python", python, file); err != nil {
		return fmt.Errorf("uv pip sync: %w", err)
	}
	if _, err := h.uvRun(w, "pip", "check", "--python", python); err != nil {
		return fmt.Errorf("uv pip check: %w", err)
	}
	frozen, err := runCapture(io.Discard, w.dir, uvEnv(h.env(w), h.root), h.uv(), "pip", "freeze", "--python", python)
	if err != nil {
		return fmt.Errorf("uv pip freeze: %w", err)
	}
	return checkFrozen(frozen, pins)
}

// seedPackages come with every venv, and uv pip sync keeps them.
var seedPackages = map[string]bool{"pip": true, "setuptools": true, "wheel": true}

// checkFrozen fails when the venv holds a package at a version pins do not
// list; markers are uv's to evaluate, so a pin may be absent here.
func checkFrozen(frozen string, pins []string) error {
	want := map[string]bool{}
	pinned := map[string]bool{}
	for _, p := range pins {
		req, _, _ := strings.Cut(p, ";")
		req = normalizeRequirement(req)
		want[req] = true
		name, _, _ := strings.Cut(req, "==")
		pinned[name] = true
	}
	for _, line := range strings.Split(frozen, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		req := normalizeRequirement(line)
		if name, _, _ := strings.Cut(req, "=="); seedPackages[name] && !pinned[name] {
			continue
		}
		if !want[req] {
			return fmt.Errorf("venv holds %s, which python_requirements does not pin", line)
		}
	}
	return nil
}

// normalizeRequirement lowercases a name==version and folds - . _ in the name,
// as PyPI does.
func normalizeRequirement(req string) string {
	name, version, _ := strings.Cut(strings.TrimSpace(req), "==")
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.NewReplacer("_", "-", ".", "-").Replace(name)
	return name + "==" + strings.TrimSpace(version)
}

func writeTemp(root, pattern, content string) (string, error) {
	dir := filepath.Join(root, "cache", "tmp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	_, werr := f.WriteString(content)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(f.Name())
		if werr != nil {
			return "", werr
		}
		return "", cerr
	}
	return f.Name(), nil
}

// SyncVenv creates venv with the flash tools' Python if it is missing, then
// makes it hold exactly pins, as syncPins does for workspace venvs.
func SyncVenv(hostBin, root, venv string, pins []string, log io.Writer) error {
	h := hostEnv{root: root, hostBin: hostBin, log: log}
	w := workspace{dir: filepath.Dir(venv), venv: venv, west: filepath.Join(venv, "bin", "west")}
	if err := ensureVenv(h.python(), venv, root, log); err != nil {
		return err
	}
	return syncPins(h, w, pins)
}
