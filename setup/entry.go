package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/matteomeneghetti/windlock/lockfile"
	"github.com/matteomeneghetti/windlock/tools"
)

// hostEnv is what every west child needs: the toolchain root, this host's
// triple, the host-tools bin, and where progress goes.
type hostEnv struct {
	root, triple, hostBin string
	log                   io.Writer
}

func (h hostEnv) python() string { return filepath.Join(h.hostBin, "python3") }

func (h hostEnv) sdkDirs(version string) (installBase, sdkDir string) {
	installBase = filepath.Join(h.root, "sdk", version, h.triple)
	return installBase, filepath.Join(installBase, "zephyr-sdk-"+version)
}

// env is the child environment for running the workspace's west.
func (h hostEnv) env(w workspace) []string { return childEnv(h.hostBin, w.venv, h.root, "") }

// workspace is a west topdir and the venv windlock keeps inside it.
type workspace struct{ dir, venv, west string }

func newWorkspace(dir string) workspace {
	venv := filepath.Join(dir, ".venv")
	return workspace{dir: dir, venv: venv, west: filepath.Join(venv, "bin", "west")}
}

// entrySpec names one Zephyr tree to prepare. An upstream tree always has a
// commit; a west.yml tree has one only when a lockfile names it.
type entrySpec struct {
	name, url, revision, commit string
	manifest                    bool
	checkTree                   func(zephyrDir string) error // runs before anything is installed
	// pythonInputs and pythonPins are the entry's locked Python packages;
	// resolvePython lets init and add resolve new ones when the inputs changed.
	pythonInputs  string
	pythonPins    []string
	resolvePython bool
}

// prepareEntry brings one workspace to what its lockfile entry records and
// returns that entry and its west version; init, add and sync all use it.
func prepareEntry(h hostEnv, appDir string, spec entrySpec, westSpec string, boards []string) (lockfile.Zephyr, string, error) {
	bootWest, err := bootstrapWest(h, westSpec)
	if err != nil {
		return lockfile.Zephyr{}, "", err
	}
	w, err := ensureWorkspace(h, appDir, spec, bootWest)
	if err != nil {
		return lockfile.Zephyr{}, "", err
	}
	zephyrDir := filepath.Join(w.dir, "zephyr")
	if spec.checkTree != nil {
		if err := spec.checkTree(zephyrDir); err != nil {
			return lockfile.Zephyr{}, "", err
		}
	}
	westVersion, pyInputs, pyPins, err := setupPython(h, w, appDir, spec, westSpec)
	if err != nil {
		return lockfile.Zephyr{}, "", err
	}
	commit, err := revParse(zephyrDir)
	if err != nil {
		return lockfile.Zephyr{}, "", err
	}
	if spec.commit != "" && commit != spec.commit {
		return lockfile.Zephyr{}, "", fmt.Errorf("zephyr %s commit is %s, want %s", spec.name, commit, spec.commit)
	}
	fmt.Fprintf(h.log, "%s: zephyr commit %s\n", spec.name, commit)
	sdk, err := readSDKVersion(zephyrDir)
	if err != nil {
		return lockfile.Zephyr{}, "", err
	}
	// The freeze comes before the toolchains because module board roots come from it.
	freeze, projects, err := freezeManifest(h, w)
	if err != nil {
		return lockfile.Zephyr{}, "", err
	}
	toolchains, err := toolchainsForBoards(boardRoots(appDir, w.dir, projects), boards)
	if err != nil {
		return lockfile.Zephyr{}, "", err
	}
	fmt.Fprintf(h.log, "%s: SDK_VERSION %s, toolchains %v\n", spec.name, sdk, toolchains)
	if err := installSDK(h, w, sdk, toolchains); err != nil {
		return lockfile.Zephyr{}, "", err
	}
	if installBase, sdkDir := h.sdkDirs(sdk); !SDKInstalled(sdkDir, sdk) {
		_ = removeIncompleteSDK(installBase, sdk)
		return lockfile.Zephyr{}, "", fmt.Errorf("%s does not hold SDK %s after install", sdkDir, sdk)
	}
	sum := sha256.Sum256([]byte(freeze))
	return lockfile.Zephyr{
		Name:           spec.name,
		URL:            spec.url,
		Revision:       spec.revision,
		Commit:         commit,
		SDK:            sdk,
		Toolchains:     toolchains,
		ManifestSHA256: hex.EncodeToString(sum[:]),
		Projects:       projects,

		PythonInputsSHA256: pyInputs,
		PythonRequirements: pyPins,
	}, westVersion, nil
}

// bootstrapWest returns the west that creates workspaces, from a venv in the cache.
func bootstrapWest(h hostEnv, westSpec string) (string, error) {
	bootstrap := filepath.Join(h.root, "cache", "bootstrap-venv")
	if err := os.MkdirAll(filepath.Join(h.root, "cache", "pip"), 0o755); err != nil {
		return "", err
	}
	if err := ensureVenv(h.python(), bootstrap, h.root, h.log); err != nil {
		return "", err
	}
	if err := pipInstall(bootstrap, h.root, westSpec, h.log); err != nil {
		return "", err
	}
	return filepath.Join(bootstrap, "bin", "west"), nil
}

// ensureWorkspace creates or reuses the workspace for spec.
func ensureWorkspace(h hostEnv, appDir string, spec entrySpec, bootWest string) (workspace, error) {
	if spec.manifest {
		plan, err := planWorkspace(h.root, appDir)
		if err != nil {
			return workspace{}, err
		}
		return newWorkspace(plan.dir), ensureManifestWorkspace(h, plan, appDir, bootWest)
	}
	dir := filepath.Join(h.root, "workspaces", spec.commit)
	return newWorkspace(dir), ensureWestWorkspace(h, dir, spec, bootWest)
}

// setupPython installs westSpec in the workspace venv, runs west update, and
// makes the venv hold exactly the entry's pinned Python packages.
func setupPython(h hostEnv, w workspace, appDir string, spec entrySpec, westSpec string) (westVersion, inputs string, pins []string, err error) {
	if err := ensureVenv(h.python(), w.venv, h.root, h.log); err != nil {
		return "", "", nil, err
	}
	fmt.Fprintf(h.log, "uv pip install %s\n", westSpec)
	if _, err := h.uvRun(w, "pip", "install", "--python", filepath.Join(w.venv, "bin", "python"), westSpec); err != nil {
		return "", "", nil, fmt.Errorf("uv pip install %s: %w", westSpec, err)
	}
	env := h.env(w)
	westVersion, err = readWestVersion(w.west, env)
	if err != nil {
		return "", "", nil, err
	}
	fmt.Fprintf(h.log, "%s: west %s\n", spec.name, westVersion)
	fmt.Fprintf(h.log, "%s: west update\n", spec.name)
	if err := runStreaming(h.log, w.dir, env, w.west, "update"); err != nil {
		return "", "", nil, fmt.Errorf("west update: %w", err)
	}
	inputs, pins, err = pythonPins(h, w, appDir, spec, westVersion)
	if err != nil {
		return "", "", nil, err
	}
	if err := syncPins(h, w, pins); err != nil {
		return "", "", nil, err
	}
	return westVersion, inputs, pins, nil
}

// freezeManifest returns west manifest --freeze --active-only and its projects;
// inactive projects such as canopennode are not cloned and cannot be frozen.
func freezeManifest(h hostEnv, w workspace) (string, []lockfile.Project, error) {
	freeze, err := runCapture(io.Discard, w.dir, h.env(w), w.west, "manifest", "--freeze", "--active-only")
	if err != nil {
		return "", nil, fmt.Errorf("west manifest --freeze: %w", err)
	}
	projects, err := parseFreeze(freeze)
	return freeze, projects, err
}

// parseFreeze returns the projects in west manifest --freeze output, in
// manifest order. A project without a path lives at its name.
func parseFreeze(text string) ([]lockfile.Project, error) {
	var doc struct {
		Manifest struct {
			Projects []struct {
				Name     string `yaml:"name"`
				Path     string `yaml:"path"`
				Revision string `yaml:"revision"`
			} `yaml:"projects"`
		} `yaml:"manifest"`
	}
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, fmt.Errorf("west manifest --freeze: %w", err)
	}
	out := []lockfile.Project{}
	for _, p := range doc.Manifest.Projects {
		if !isHex(p.Revision, 40) {
			return nil, fmt.Errorf("west manifest --freeze: project %s revision %q is not a commit", p.Name, p.Revision)
		}
		path := p.Path
		if path == "" {
			path = p.Name
		}
		out = append(out, lockfile.Project{Name: p.Name, Path: path, Revision: p.Revision})
	}
	return out, nil
}

// fetchSums reads the sha256.sum of each SDK release. It downloads no bundle.
// It is a variable so tests can run without the network.
var fetchSums = fetchReleaseSums

func fetchReleaseSums(versions []string, log io.Writer) (map[string]string, error) {
	out := map[string]string{}
	for _, version := range versions {
		fmt.Fprintf(log, "reading sha256.sum for SDK %s\n", version)
		text, err := tools.FetchText(sdkReleaseURL(version, "sha256.sum"), 2<<20)
		if err != nil {
			return nil, err
		}
		out[version] = text
	}
	return out, nil
}
