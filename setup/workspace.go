package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// workspacePlan is the west workspace for a project whose west.yml is the
// manifest; init runs in dir.
type workspacePlan struct {
	dir      string
	init     []string
	selfPath string
}

// WorkspaceDir is appDir's west topdir: its own directory for a west.yml
// project, else the shared checkout of the Zephyr commit.
func WorkspaceDir(root, appDir, commit string) (string, error) {
	abs, err := filepath.Abs(appDir)
	if err != nil {
		return "", err
	}
	if westYAMLExists(abs) {
		if _, err := readSelfPath(filepath.Join(abs, "west.yml")); err != nil {
			return "", err
		}
		return manifestDir(root, abs), nil
	}
	if commit == "" {
		return "", fmt.Errorf("upstream workspace needs a zephyr commit")
	}
	return filepath.Join(root, "workspaces", commit), nil
}

// planWorkspace plans `west init -l` for a project that has west.yml.
func planWorkspace(root, appDir string) (workspacePlan, error) {
	abs, err := filepath.Abs(appDir)
	if err != nil {
		return workspacePlan{}, err
	}
	if !westYAMLExists(abs) {
		return workspacePlan{}, fmt.Errorf("%s has no west.yml", abs)
	}
	selfPath, err := readSelfPath(filepath.Join(abs, "west.yml"))
	if err != nil {
		return workspacePlan{}, err
	}
	return workspacePlan{
		dir:      manifestDir(root, abs),
		init:     []string{"init", "-l", selfPath},
		selfPath: selfPath,
	}, nil
}

func manifestDir(root, absApp string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(absApp)))
	return filepath.Join(root, "workspaces", "manifest", hex.EncodeToString(sum[:]))
}

func westYAMLExists(appDir string) bool {
	info, err := os.Stat(filepath.Join(appDir, "west.yml"))
	return err == nil && !info.IsDir()
}

type westYAML struct {
	Manifest struct {
		Defaults struct {
			Remote   string `yaml:"remote"`
			Revision string `yaml:"revision"`
		} `yaml:"defaults"`
		Remotes []struct {
			Name    string `yaml:"name"`
			URLBase string `yaml:"url-base"`
		} `yaml:"remotes"`
		Projects []westProject `yaml:"projects"`
		Self     struct {
			Path string `yaml:"path"`
		} `yaml:"self"`
	} `yaml:"manifest"`
}

type westProject struct {
	Name     string `yaml:"name"`
	Remote   string `yaml:"remote"`
	URL      string `yaml:"url"`
	RepoPath string `yaml:"repo-path"`
	Revision string `yaml:"revision"`
}

func readWestYAML(path string) (westYAML, error) {
	var doc westYAML
	b, err := os.ReadFile(path)
	if err != nil {
		return doc, err
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return doc, fmt.Errorf("west.yml: %w", err)
	}
	return doc, nil
}

// manifestZephyr returns the zephyr project's URL and revision from west.yml,
// resolved as west does (url over remote, then defaults, else "master").
func manifestZephyr(path string) (url, revision string, err error) {
	doc, err := readWestYAML(path)
	if err != nil {
		return "", "", err
	}
	m := doc.Manifest
	for _, p := range m.Projects {
		if p.Name != "zephyr" {
			continue
		}
		revision = p.Revision
		if revision == "" {
			revision = m.Defaults.Revision
		}
		if revision == "" {
			revision = "master"
		}
		if p.URL != "" {
			return p.URL, revision, nil
		}
		remote := p.Remote
		if remote == "" {
			remote = m.Defaults.Remote
		}
		for _, r := range m.Remotes {
			if r.Name == remote && r.URLBase != "" {
				repo := p.RepoPath
				if repo == "" {
					repo = p.Name
				}
				return strings.TrimSuffix(r.URLBase, "/") + "/" + repo, revision, nil
			}
		}
		return "", "", fmt.Errorf("west.yml zephyr project has no url and no known remote %q", remote)
	}
	return "", "", fmt.Errorf("west.yml has no project named zephyr")
}

func readSelfPath(path string) (string, error) {
	doc, err := readWestYAML(path)
	if err != nil {
		return "", err
	}
	selfPath := filepath.Clean(doc.Manifest.Self.Path)
	if doc.Manifest.Self.Path == "" || selfPath == "." || filepath.IsAbs(selfPath) || selfPath == ".." || strings.HasPrefix(selfPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("west.yml manifest.self.path %q is not a relative project path", doc.Manifest.Self.Path)
	}
	return selfPath, nil
}

// localWestDir is where `west init -l selfPath` creates .west: the workspace
// root for a one-segment selfPath, never the user's project.
func localWestDir(workspace, selfPath string) string {
	return filepath.Dir(filepath.Join(workspace, selfPath))
}

func ensureManifestWorkspace(h hostEnv, plan workspacePlan, appDir, westBin string) error {
	link := filepath.Join(plan.dir, plan.selfPath)
	westDir := localWestDir(plan.dir, plan.selfPath)
	if info, err := os.Stat(filepath.Join(westDir, ".west")); err == nil && info.IsDir() {
		fmt.Fprintf(h.log, "workspace %s\n", plan.dir)
		return ensureProjectSymlink(link, appDir)
	}
	if err := os.MkdirAll(plan.dir, 0o755); err != nil {
		return err
	}
	if err := stageLocalManifest(plan.dir, plan.selfPath, appDir); err != nil {
		return err
	}
	userWest := filepath.Join(appDir, ".west")
	parentWest := filepath.Join(filepath.Dir(appDir), ".west")
	_, userErr := os.Stat(userWest)
	_, parentErr := os.Stat(parentWest)
	fmt.Fprintf(h.log, "west init -l %s\n", plan.selfPath)
	err := runStreaming(h.log, plan.dir, childEnv(h.hostBin, "", h.root, ""), westBin, plan.init...)
	if err != nil {
		_ = os.RemoveAll(plan.dir)
		return fmt.Errorf("west init: %w", err)
	}
	if err := rejectEscapedWest(userWest, userErr == nil, parentWest, parentErr == nil, westDir); err != nil {
		_ = os.RemoveAll(plan.dir)
		return err
	}
	if _, err := os.Stat(filepath.Join(westDir, ".west")); err != nil {
		_ = os.RemoveAll(plan.dir)
		return fmt.Errorf("west init did not create %s", filepath.Join(westDir, ".west"))
	}
	if err := replaceWithProjectSymlink(plan.dir, plan.selfPath, appDir); err != nil {
		return err
	}
	return nil
}

func rejectEscapedWest(userWest string, userHad bool, parentWest string, parentHad bool, westDir string) error {
	if !userHad {
		if _, err := os.Stat(userWest); err == nil {
			_ = os.RemoveAll(userWest)
			return fmt.Errorf("west init wrote .west into the project")
		}
	}
	owned := filepath.Clean(filepath.Join(westDir, ".west"))
	if !parentHad && filepath.Clean(parentWest) != owned {
		if _, err := os.Stat(parentWest); err == nil {
			_ = os.RemoveAll(parentWest)
			return fmt.Errorf("west init wrote .west next to the project")
		}
	}
	return nil
}

func stageLocalManifest(workspace, selfPath, appDir string) error {
	dest := filepath.Join(workspace, selfPath)
	if err := insideWorkspace(workspace, dest); err != nil {
		return err
	}
	if info, err := os.Lstat(dest); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(dest); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("%s already exists", dest)
		}
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(appDir, "west.yml"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dest, "west.yml"), b, 0o644)
}

func replaceWithProjectSymlink(workspace, selfPath, appDir string) error {
	dest := filepath.Join(workspace, selfPath)
	if err := insideWorkspace(workspace, dest); err != nil {
		return err
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return os.Symlink(appDir, dest)
}

func ensureProjectSymlink(link, appDir string) error {
	target, err := os.Readlink(link)
	if err == nil {
		if filepath.Clean(target) == filepath.Clean(appDir) {
			return nil
		}
		return fmt.Errorf("symlink %s points at %s, want %s", link, target, appDir)
	}
	if _, statErr := os.Lstat(link); statErr == nil {
		return fmt.Errorf("%s exists and is not the project symlink", link)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	return os.Symlink(appDir, link)
}

func insideWorkspace(workspace, path string) error {
	rel, err := filepath.Rel(workspace, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s is outside %s", path, workspace)
	}
	return nil
}
