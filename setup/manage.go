package setup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/matteomeneghetti/windlock/host"
	"github.com/matteomeneghetti/windlock/lockfile"
	"github.com/matteomeneghetti/windlock/tools"
)

// managed is a project whose windlock.yml and windlock.lock.yml agree, ready to
// be changed by one target or zephyr command.
type managed struct {
	appDir, cfgPath, lockPath string
	cfg                       lockfile.Config
	lock                      lockfile.Lockfile
	manifest                  bool
	h                         hostEnv
}

// loadManaged reads both files and fails when either is missing or they no
// longer agree, so a command never builds on a stale pair.
func loadManaged(appDir string, log io.Writer) (managed, error) {
	appDir, err := filepath.Abs(appDir)
	if err != nil {
		return managed{}, err
	}
	m := managed{
		appDir:   appDir,
		cfgPath:  filepath.Join(appDir, lockfile.ConfigFilename),
		lockPath: filepath.Join(appDir, lockfile.Filename),
		manifest: westYAMLExists(appDir),
	}
	if m.cfg, err = lockfile.ReadConfigFile(m.cfgPath); err != nil {
		return managed{}, fmt.Errorf("%w; run windlock init first", err)
	}
	if m.lock, err = lockfile.ReadFile(m.lockPath); err != nil {
		if os.IsNotExist(err) {
			return managed{}, fmt.Errorf("%w; run windlock init first", err)
		}
		return managed{}, err
	}
	if err := m.lock.CheckSchema(); err != nil {
		return managed{}, err
	}
	if err := configMatches(m.cfg, m.lock, m.manifest); err != nil {
		return managed{}, err
	}
	triple, err := host.Current()
	if err != nil {
		return managed{}, err
	}
	root, err := tools.Root()
	if err != nil {
		return managed{}, err
	}
	bin, err := HostToolsBin(m.lock, root, triple)
	if err != nil {
		return managed{}, err
	}
	m.h = hostEnv{root: root, triple: triple, hostBin: bin, log: log}
	return m, nil
}

// save writes cfg, then lock. Both are computed before either is written.
func (m managed) save(cfg lockfile.Config, lock lockfile.Lockfile) error {
	if err := lockfile.WriteConfigFile(m.cfgPath, cfg); err != nil {
		return err
	}
	if err := lockfile.WriteFile(m.lockPath, lock); err != nil {
		return err
	}
	fmt.Fprintf(m.h.log, "wrote %s and %s\n", m.cfgPath, m.lockPath)
	return nil
}

// relock recomputes each entry's toolchains for cfg's boards, installs any
// missing, and reassembles the lockfile, without cloning or west update.
func (m managed) relock(cfg lockfile.Config, lock lockfile.Lockfile) (lockfile.Lockfile, error) {
	entries := append([]lockfile.Zephyr(nil), lock.Zephyr...)
	for i, entry := range entries {
		ws, err := WorkspaceDir(m.h.root, m.appDir, entry.Commit)
		if err != nil {
			return lockfile.Lockfile{}, err
		}
		zephyrDir := filepath.Join(ws, "zephyr")
		if info, err := os.Stat(zephyrDir); err != nil || !info.IsDir() {
			return lockfile.Lockfile{}, fmt.Errorf("zephyr %s is not installed at %s; run windlock sync", entry.Name, zephyrDir)
		}
		toolchains, err := toolchainsForBoards(boardRoots(m.appDir, ws, entry.Projects), cfg.Boards())
		if err != nil {
			return lockfile.Lockfile{}, err
		}
		if _, sdkDir := m.h.sdkDirs(entry.SDK); len(missingToolchains(sdkDir, toolchains)) > 0 || !SDKInstalled(sdkDir, entry.SDK) {
			if err := requireHostBin(m.h.hostBin); err != nil {
				return lockfile.Lockfile{}, err
			}
			if err := installSDK(m.h, newWorkspace(ws), entry.SDK, toolchains); err != nil {
				return lockfile.Lockfile{}, err
			}
		}
		entry.Toolchains = toolchains
		entries[i] = entry
	}
	return lockEntries(entries, lock.West, m.h.triple, m.h.log)
}

// TargetAdd adds a build target to windlock.yml and locks the toolchains its
// board needs. source is a directory inside the project.
func TargetAdd(appDir, name, board, source string, opts TargetOptions, log io.Writer) error {
	m, err := loadManaged(appDir, log)
	if err != nil {
		return err
	}
	if err := requireProjectDir(m.appDir, source); err != nil {
		return fmt.Errorf("target %s source: %w", name, err)
	}
	source = filepath.ToSlash(filepath.Clean(source))
	sysbuild, why := detectSysbuild(m.appDir, source)
	if opts.Sysbuild != nil {
		sysbuild, why = *opts.Sysbuild, "set by flag"
	}
	cfg := m.cfg
	cfg.Targets = copyTargets(cfg.Targets)
	if err := cfg.AddTarget(name, lockfile.Target{Board: board, Source: source, Sysbuild: sysbuild}); err != nil {
		return err
	}
	if opts.Default {
		cfg.Target = name
	}
	lock, err := m.relock(cfg, m.lock)
	if err != nil {
		return err
	}
	if err := m.save(cfg, lock); err != nil {
		return err
	}
	fmt.Fprintf(log, "target %s: board %s, source %s\n", name, board, source)
	if sysbuild {
		fmt.Fprintf(log, "target %s: sysbuild on (%s); windlock build %s passes --sysbuild\n", name, why, name)
	} else if why != "" {
		fmt.Fprintf(log, "target %s: sysbuild off (%s)\n", name, why)
	}
	return nil
}

// TargetOptions are the choices target add takes besides board and source.
type TargetOptions struct {
	Default bool
	// Sysbuild forces sysbuild on or off. nil detects it from the source.
	Sysbuild *bool
}

// detectSysbuild reports whether the app at source has sysbuild files, which
// Zephyr reads only in a sysbuild build, and which file says so.
func detectSysbuild(appDir, source string) (bool, string) {
	for _, name := range []string{"sysbuild.conf", "Kconfig.sysbuild", "sysbuild"} {
		if _, err := os.Stat(filepath.Join(appDir, filepath.FromSlash(source), name)); err == nil {
			return true, "found " + source + "/" + name
		}
	}
	return false, ""
}

// TargetRemove removes a build target and relocks. Toolchains no target
// needs leave the lockfile but stay installed.
func TargetRemove(appDir, name string, log io.Writer) error {
	m, err := loadManaged(appDir, log)
	if err != nil {
		return err
	}
	cfg := m.cfg
	cfg.Targets = copyTargets(cfg.Targets)
	if err := cfg.RemoveTarget(name); err != nil {
		return err
	}
	lock, err := m.relock(cfg, m.lock)
	if err != nil {
		return err
	}
	if err := m.save(cfg, lock); err != nil {
		return err
	}
	if cfg.Target == "" && len(cfg.Targets) > 0 {
		fmt.Fprintf(log, "%s was the default target; set another with windlock target default <name>\n", name)
	}
	return nil
}

// TargetDefault makes name the target build uses when given none. The
// lockfile does not depend on it, so only windlock.yml is written.
func TargetDefault(appDir, name string, log io.Writer) error {
	m, err := loadManaged(appDir, log)
	if err != nil {
		return err
	}
	cfg := m.cfg
	if err := cfg.SetDefaultTarget(name); err != nil {
		return err
	}
	if err := lockfile.WriteConfigFile(m.cfgPath, cfg); err != nil {
		return err
	}
	fmt.Fprintf(log, "default target %s\n", name)
	return nil
}

// TargetList prints one line per target, the default marked with *.
func TargetList(appDir string, out, log io.Writer) error {
	m, err := loadManaged(appDir, log)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(m.cfg.Targets))
	for name := range m.cfg.Targets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t := m.cfg.Targets[name]
		line := fmt.Sprintf("%s %s  %s  %s", mark(name == m.cfg.Target), name, t.Board, t.Source)
		if t.Sysbuild {
			line += "  sysbuild"
		}
		fmt.Fprintln(out, line)
	}
	return nil
}

// ZephyrRemove drops a non-default revision from windlock.yml and the
// lockfile. Its workspace and SDK stay installed.
func ZephyrRemove(appDir, revision string, log io.Writer) error {
	m, i, err := loadZephyr(appDir, revision, log)
	if err != nil {
		return err
	}
	if i == 0 {
		return fmt.Errorf("zephyr %s is the default; make another the default with windlock zephyr default first", m.lock.Zephyr[0].Name)
	}
	cfg, lock := m.cfg, m.lock
	cfg.Zephyr = append([]string(nil), cfg.Zephyr...)
	if err := cfg.RemoveZephyr(i); err != nil {
		return err
	}
	lock.RemoveZephyr(i)
	next, err := lockEntries(lock.Zephyr, lock.West, m.h.triple, log)
	if err != nil {
		return err
	}
	return m.save(cfg, next)
}

// ZephyrDefault makes a revision the default, the one build uses.
func ZephyrDefault(appDir, revision string, log io.Writer) error {
	m, i, err := loadZephyr(appDir, revision, log)
	if err != nil {
		return err
	}
	if i == 0 {
		fmt.Fprintf(log, "zephyr %s is already the default\n", m.lock.Zephyr[0].Name)
		return nil
	}
	cfg, lock := m.cfg, m.lock
	cfg.Zephyr = append([]string(nil), cfg.Zephyr...)
	if err := cfg.MoveZephyrFirst(i); err != nil {
		return err
	}
	lock.MoveZephyrFirst(i)
	next, err := lockEntries(lock.Zephyr, lock.West, m.h.triple, log)
	if err != nil {
		return err
	}
	if err := m.save(cfg, next); err != nil {
		return err
	}
	fmt.Fprintf(log, "default zephyr %s\n", next.Zephyr[0].Name)
	return nil
}

// ZephyrList prints one line per locked revision, the default marked with *.
func ZephyrList(appDir string, out, log io.Writer) error {
	m, err := loadManaged(appDir, log)
	if err != nil {
		return err
	}
	for i, e := range m.lock.Zephyr {
		commit := e.Commit
		if len(commit) > 8 {
			commit = commit[:8]
		}
		fmt.Fprintf(out, "%s %s  %s  %s  sdk %s\n", mark(i == 0), e.Name, e.Revision, commit, e.SDK)
	}
	return nil
}

// loadZephyr loads the project and finds the entry named by revision, as
// either its name (4.3.1) or its revision (v4.3.1).
func loadZephyr(appDir, revision string, log io.Writer) (managed, int, error) {
	m, err := loadManaged(appDir, log)
	if err != nil {
		return managed{}, 0, err
	}
	if m.manifest {
		return managed{}, 0, fmt.Errorf("west.yml is the manifest of this project and names its one Zephyr revision; change it there, then rerun windlock init")
	}
	name, ref, err := parseAddRevision(revision)
	if err != nil {
		return managed{}, 0, err
	}
	for i, e := range m.lock.Zephyr {
		if e.Name == name || e.Revision == ref {
			return m, i, nil
		}
	}
	return managed{}, 0, fmt.Errorf("%s has no zephyr %s", lockfile.Filename, revision)
}

// requireProjectDir fails unless rel is a relative path to a directory
// inside appDir.
func requireProjectDir(appDir, rel string) error {
	if rel == "" || filepath.IsAbs(rel) {
		return fmt.Errorf("%q is not a path relative to the project", rel)
	}
	dir := filepath.Join(appDir, rel)
	if err := insideWorkspace(appDir, dir); err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	return nil
}

func copyTargets(in map[string]lockfile.Target) map[string]lockfile.Target {
	if in == nil {
		return nil
	}
	out := make(map[string]lockfile.Target, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mark(isDefault bool) string {
	if isDefault {
		return "*"
	}
	return " "
}
