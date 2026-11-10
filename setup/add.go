package setup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/matteomeneghetti/windlock/lockfile"
	"github.com/matteomeneghetti/windlock/tools"
)

// ZephyrAdd installs another Zephyr tag or branch beside the default and
// relocks; refused for west.yml projects, whose manifest names the revision.
func ZephyrAdd(appDir, revision string, log io.Writer) error {
	name, ref, err := parseAddRevision(revision)
	if err != nil {
		return err
	}
	appDir, err = filepath.Abs(appDir)
	if err != nil {
		return err
	}
	if westYAMLExists(appDir) {
		return fmt.Errorf("zephyr add does not apply to a west.yml project: west.yml is the manifest and names the Zephyr revision. Change the zephyr project revision in %s to %s, then rerun windlock init",
			filepath.Join(appDir, "west.yml"), ref)
	}
	m, err := loadManaged(appDir, log)
	if err != nil {
		return err
	}
	cfg, lock, h := m.cfg, m.lock, m.h
	if err := requireHostBin(h.hostBin); err != nil {
		return err
	}
	for _, entry := range lock.Zephyr {
		if entry.Name == name {
			return fmt.Errorf("zephyr %s is already in the lockfile", name)
		}
	}
	fmt.Fprintf(log, "resolving %s\n", ref)
	commit, err := lookupCommit(zephyrURL, ref)
	if err != nil {
		return err
	}
	fmt.Fprintf(log, "zephyr commit %s\n", commit)
	for _, entry := range lock.Zephyr {
		if entry.Commit == commit {
			return fmt.Errorf("%s resolved to the existing commit %s", ref, commit)
		}
	}
	fmt.Fprintf(log, "reusing build tools %s\n", h.hostBin)
	spec := entrySpec{
		name: name, url: zephyrURL, revision: ref, commit: commit,
		checkTree:     func(zephyrDir string) error { return floorsMatch(zephyrDir, h.root, lock, h.triple) },
		resolvePython: true,
	}
	entry, _, err := prepareEntry(h, m.appDir, spec, "west=="+lock.West, cfg.Boards())
	if err != nil {
		return err
	}
	next, err := lockEntries(append(append([]lockfile.Zephyr(nil), lock.Zephyr...), entry), lock.West, h.triple, lockedPins(lock), log)
	if err != nil {
		return err
	}
	cfg.Zephyr = append(append([]string(nil), cfg.Zephyr...), ref)
	return m.save(cfg, next)
}

func requireHostBin(bin string) error {
	for _, name := range []string{"cmake", "python3", "dtc"} {
		if _, err := os.Stat(filepath.Join(bin, name)); err != nil {
			return fmt.Errorf("build tools bin %s: %w", bin, err)
		}
	}
	return nil
}

var (
	cmakeFloorRE  = regexp.MustCompile(`cmake_minimum_required\(VERSION\s+([0-9.]+)\)`)
	pythonFloorRE = regexp.MustCompile(`PYTHON_MINIMUM_REQUIRED\s+([0-9.]+)`)
	dtcFloorRE    = regexp.MustCompile(`find_package\(Dtc\s+([0-9.]+)\)`)
)

// floorsMatch reuses the installed build tools when this tree's CMake,
// Python, and dtc floors are within the versions lock pins for this host.
func floorsMatch(zephyrDir, root string, lock lockfile.Lockfile, triple string) error {
	set, err := BuildSet(lock, triple)
	if err != nil {
		return err
	}
	have := map[string]string{}
	for _, t := range set.Tools {
		have[t.Name] = t.Version
	}
	checks := []struct {
		file, reName, tool string
		re                 *regexp.Regexp
	}{
		{"samples/hello_world/CMakeLists.txt", "cmake", "cmake", cmakeFloorRE},
		{"cmake/modules/python.cmake", "python", "python", pythonFloorRE},
		{"cmake/modules/dts.cmake", "dtc", "dtc", dtcFloorRE},
	}
	for _, check := range checks {
		text, err := os.ReadFile(filepath.Join(zephyrDir, check.file))
		if err != nil {
			return err
		}
		m := check.re.FindStringSubmatch(string(text))
		if m == nil {
			return fmt.Errorf("no %s floor in %s", check.reName, check.file)
		}
		ok, err := tools.AtLeast(have[check.tool], m[1])
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%s floor %s is above the pinned %s", check.reName, m[1], have[check.tool])
		}
	}
	if _, err := os.Stat(filepath.Join(root, "host")); err != nil {
		return err
	}
	return nil
}
