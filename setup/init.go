// Package setup installs, locks and manages a project's Zephyr workspaces, SDKs and targets.
package setup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"windlock/host"
	"windlock/lockfile"
	"windlock/tools"
)

const (
	zephyrURL      = "https://github.com/zephyrproject-rtos/zephyr"
	zephyrRevision = "v4.4.2"
	westFloor      = "west>=0.14.0"
)

// Init installs everything windlock.yml (or west.yml) names and writes
// windlock.lock.yml, creating windlock.yml with revision when it is missing.
func Init(appDir, revision string, log io.Writer) error {
	appDir, err := filepath.Abs(appDir)
	if err != nil {
		return err
	}
	triple, err := host.Current()
	if err != nil {
		return err
	}
	root, err := tools.Root()
	if err != nil {
		return err
	}
	cfg, prev, err := loadConfig(appDir, revision, log)
	if err != nil {
		return err
	}
	fmt.Fprintf(log, "installing host tools\n")
	if err := tools.Install(log); err != nil {
		return err
	}
	id, err := tools.CurrentID()
	if err != nil {
		return err
	}
	h := hostEnv{root: root, triple: triple, hostBin: tools.BinDir(root, id), log: log}
	if err := tools.CheckPython312(h.python()); err != nil {
		return err
	}
	westSpec := westFloor
	if prev.West != "" {
		westSpec = "west==" + prev.West
		fmt.Fprintf(log, "%s pins west %s\n", lockfile.Filename, prev.West)
	}
	specs, err := configSpecs(appDir, cfg, log)
	if err != nil {
		return err
	}
	var entries []lockfile.Zephyr
	west := ""
	for _, spec := range specs {
		// Locked pins are kept while their requirement files are unchanged.
		spec.resolvePython = true
		for _, old := range prev.Zephyr {
			if old.Name == spec.name {
				spec.pythonInputs, spec.pythonPins = old.PythonInputsSHA256, old.PythonRequirements
			}
		}
		entry, got, err := prepareEntry(h, appDir, spec, westSpec, cfg.Boards())
		if err != nil {
			return err
		}
		if west == "" {
			west = got
			westSpec = "west==" + got
		}
		entries = append(entries, entry)
	}
	lock, err := lockEntries(entries, west, triple, log)
	if err != nil {
		return err
	}
	path := filepath.Join(appDir, lockfile.Filename)
	if err := lockfile.WriteFile(path, lock); err != nil {
		return err
	}
	fmt.Fprintf(log, "wrote %s\n", path)
	return nil
}

// lockEntries reads the sha256.sum of every SDK the entries use and
// assembles the lockfile.
func lockEntries(entries []lockfile.Zephyr, west, triple string, log io.Writer) (lockfile.Lockfile, error) {
	sums, err := fetchSums(lockfile.Lockfile{Zephyr: entries}.SDKs(), log)
	if err != nil {
		return lockfile.Lockfile{}, err
	}
	return assembleLock(entries, west, sums, triple)
}

// loadConfig reads or creates windlock.yml (with revision, if given) and returns
// it with the existing lockfile, or an empty one.
func loadConfig(appDir, revision string, log io.Writer) (lockfile.Config, lockfile.Lockfile, error) {
	cfgPath := filepath.Join(appDir, lockfile.ConfigFilename)
	lock, err := lockfile.ReadFile(filepath.Join(appDir, lockfile.Filename))
	if err != nil && !os.IsNotExist(err) {
		return lockfile.Config{}, lockfile.Lockfile{}, err
	}
	manifest := westYAMLExists(appDir)
	explicit := revision != ""
	if explicit && manifest {
		return lockfile.Config{}, lockfile.Lockfile{}, fmt.Errorf("--zephyr does not apply to a west.yml project: west.yml names the Zephyr revision")
	}
	if !explicit {
		revision = zephyrRevision
	}
	cfg, err := lockfile.ReadConfigFile(cfgPath)
	if explicit && err == nil {
		return lockfile.Config{}, lockfile.Lockfile{}, fmt.Errorf("%s already names its zephyr revisions; use windlock zephyr add and windlock zephyr default instead of --zephyr", lockfile.ConfigFilename)
	}
	switch {
	case err == nil:
	case !os.IsNotExist(err):
		return lockfile.Config{}, lockfile.Lockfile{}, err
	default:
		if !manifest {
			cfg.Zephyr = []string{revision}
		}
		if err := lockfile.WriteConfigFile(cfgPath, cfg); err != nil {
			return lockfile.Config{}, lockfile.Lockfile{}, err
		}
		fmt.Fprintf(log, "wrote %s\n", cfgPath)
	}
	if manifest && len(cfg.Zephyr) > 0 {
		return lockfile.Config{}, lockfile.Lockfile{}, fmt.Errorf("%s lists zephyr revisions, but west.yml is the manifest and names the revision; remove zephyr from %s", lockfile.ConfigFilename, cfgPath)
	}
	if !manifest && len(cfg.Zephyr) == 0 {
		return lockfile.Config{}, lockfile.Lockfile{}, fmt.Errorf("%s lists no zephyr revision", cfgPath)
	}
	return cfg, lock, nil
}

// configSpecs resolves what windlock.yml, or west.yml, asks for.
func configSpecs(appDir string, cfg lockfile.Config, log io.Writer) ([]entrySpec, error) {
	if westYAMLExists(appDir) {
		manifest := filepath.Join(appDir, "west.yml")
		if _, err := readSelfPath(manifest); err != nil {
			return nil, err
		}
		repo, revision, err := manifestZephyr(manifest)
		if err != nil {
			return nil, err
		}
		name, _, err := parseAddRevision(revision)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(log, "manifest west.yml: zephyr %s\n", revision)
		return []entrySpec{{name: name, url: repo, revision: revision, manifest: true}}, nil
	}
	var specs []entrySpec
	names := map[string]bool{}
	commits := map[string]string{}
	for _, revision := range cfg.Zephyr {
		name, ref, err := parseAddRevision(revision)
		if err != nil {
			return nil, err
		}
		if names[name] {
			return nil, fmt.Errorf("%s lists zephyr %s twice", lockfile.ConfigFilename, name)
		}
		names[name] = true
		fmt.Fprintf(log, "resolving %s\n", ref)
		commit, err := lookupCommit(zephyrURL, ref)
		if err != nil {
			return nil, err
		}
		if other, ok := commits[commit]; ok {
			return nil, fmt.Errorf("%s and %s resolve to the same commit %s", other, ref, commit)
		}
		commits[commit] = ref
		specs = append(specs, entrySpec{name: name, url: zephyrURL, revision: ref, commit: commit})
	}
	return specs, nil
}
