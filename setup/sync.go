package setup

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/matteomeneghetti/windlock/host"
	"github.com/matteomeneghetti/windlock/lockfile"
	"github.com/matteomeneghetti/windlock/tools"
)

// Sync installs what windlock.lock.yml names for this host, then fails with a
// field diff unless the lockfile it regenerates is the one on disk.
func Sync(appDir string, log io.Writer) error {
	appDir, err := filepath.Abs(appDir)
	if err != nil {
		return err
	}
	cfg, err := lockfile.ReadConfigFile(filepath.Join(appDir, lockfile.ConfigFilename))
	if err != nil {
		return fmt.Errorf("%w; run windlock init", err)
	}
	lock, err := lockfile.ReadFile(filepath.Join(appDir, lockfile.Filename))
	if err != nil {
		return err
	}
	if err := lock.CheckSchema(); err != nil {
		return err
	}
	manifest := westYAMLExists(appDir)
	if err := configMatches(cfg, lock, manifest); err != nil {
		return err
	}
	triple, err := host.Current()
	if err != nil {
		return err
	}
	if err := usablePlatform(lock, triple); err != nil {
		return err
	}
	set, err := BuildSet(lock, triple)
	if err != nil {
		return err
	}
	if err := tools.Validate(set); err != nil {
		return err
	}
	sums, err := fetchSums(lock.SDKs(), log)
	if err != nil {
		return err
	}
	if err := checkBundles(lock, sums, triple, log); err != nil {
		return err
	}
	root, err := tools.Root()
	if err != nil {
		return err
	}
	fmt.Fprintf(log, "installing build tools\n")
	bin, err := tools.Install(set, log)
	if err != nil {
		return err
	}
	h := hostEnv{root: root, triple: triple, hostBin: bin, log: log}
	var observed []lockfile.Zephyr
	for _, entry := range lock.Zephyr {
		spec := entrySpec{
			name: entry.Name, url: entry.URL, revision: entry.Revision, commit: entry.Commit, manifest: manifest,
			pythonInputs: entry.PythonInputsSHA256, pythonPins: entry.PythonRequirements,
		}
		got, _, err := prepareEntry(h, appDir, spec, "west=="+lock.West, cfg.Boards())
		if err != nil {
			return err
		}
		observed = append(observed, got)
	}
	// The pins are the lockfile's own: what sync checks is what this host
	// observed, the Zephyr trees, Python packages and SDK bundles.
	regenerated, err := assembleLock(observed, lock.West, sums, lockedPins(lock), triple)
	if err != nil {
		return err
	}
	diff, err := lockfile.Diff(lock, regenerated)
	if err != nil {
		return err
	}
	if len(diff) > 0 {
		return fmt.Errorf("this host does not reproduce %s (want is the file, got is this host):\n  %s\nIf %s or the upstream trees changed on purpose, rerun windlock init",
			lockfile.Filename, strings.Join(diff, "\n  "), lockfile.ConfigFilename)
	}
	fmt.Fprintf(log, "sync complete\n")
	return nil
}

// configMatches fails when windlock.yml and the lockfile name different
// revisions, e.g. one was committed without the other.
func configMatches(cfg lockfile.Config, lock lockfile.Lockfile, manifest bool) error {
	if manifest {
		if len(cfg.Zephyr) > 0 {
			return fmt.Errorf("%s lists zephyr revisions, but west.yml is the manifest; remove them", lockfile.ConfigFilename)
		}
		return nil
	}
	var locked []string
	for _, e := range lock.Zephyr {
		locked = append(locked, e.Revision)
	}
	if strings.Join(cfg.Zephyr, " ") != strings.Join(locked, " ") {
		return fmt.Errorf("%s lists zephyr [%s], %s locks [%s]; rerun windlock init",
			lockfile.ConfigFilename, strings.Join(cfg.Zephyr, " "), lockfile.Filename, strings.Join(locked, " "))
	}
	return nil
}

// checkBundles checks this host's locked bundles against each release
// sha256.sum, the same file west sdk install verifies the download with.
func checkBundles(lock lockfile.Lockfile, sums map[string]string, triple string, log io.Writer) error {
	p, err := lock.Platform(triple)
	if err != nil {
		return err
	}
	for _, version := range lock.SDKs() {
		if err := checkBundle(p.SDK[version], sums[version], version, triple); err != nil {
			return err
		}
		fmt.Fprintf(log, "sdk %s bundle for %s matches sha256.sum\n", version, triple)
	}
	return nil
}

func checkBundle(locked lockfile.Bundle, sumText, version, triple string) error {
	name, sum, err := selectMinimalBundle(sumText, version, triple)
	if err != nil {
		return err
	}
	if locked.Filename != name || !strings.EqualFold(locked.SHA256, sum) {
		return fmt.Errorf("%s records SDK %s bundle %s %s for %s, sha256.sum lists %s %s",
			lockfile.Filename, version, locked.Filename, locked.SHA256, triple, name, sum)
	}
	return nil
}
