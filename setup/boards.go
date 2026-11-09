package setup

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/matteomeneghetti/windlock/lockfile"
)

// boardTarget is one buildable board target, as its Twister YAML records it.
// A board directory has one Twister file per target, beside board.yml.
type boardTarget struct {
	Identifier string // nrf5340dk/nrf5340/cpuapp, the name west build -b takes
	Arch       string // arm, riscv, xtensa, ...
	SoC        string // the identifier's second segment, empty for a bare board
	File       string
}

// boardRoots lists the boards directories west build searches, in order: the
// project, the Zephyr tree, then each module's board_root (zephyr/module.yml).
func boardRoots(appDir, ws string, projects []lockfile.Project) []string {
	roots := []string{filepath.Join(appDir, "boards"), filepath.Join(ws, "zephyr", "boards")}
	for _, p := range projects {
		module := filepath.Join(ws, filepath.FromSlash(p.Path))
		for _, name := range []string{"module.yml", "module.yaml"} {
			raw, err := os.ReadFile(filepath.Join(module, "zephyr", name))
			if err != nil {
				continue
			}
			var doc struct {
				Build struct {
					Settings struct {
						BoardRoot string `yaml:"board_root"`
					} `yaml:"settings"`
				} `yaml:"build"`
			}
			if yaml.Unmarshal(raw, &doc) == nil && doc.Build.Settings.BoardRoot != "" {
				roots = append(roots, filepath.Join(module, filepath.FromSlash(doc.Build.Settings.BoardRoot), "boards"))
			}
			break
		}
	}
	return roots
}

// resolveBoard finds the Twister target for a west build -b argument such as
// nrf5340dk/nrf5340/cpuapp, nrf5340dk@1.0.0//cpuapp, or qemu_cortex_m3.
func resolveBoard(roots []string, board string) (boardTarget, error) {
	name, qualifiers := splitBoardTarget(board)
	if name == "" {
		return boardTarget{}, fmt.Errorf("board name is empty")
	}
	dir, socs, err := findBoardDir(roots, name)
	if err != nil {
		return boardTarget{}, err
	}
	identifier := name
	if qualifiers != "" {
		// board//cluster leaves out the SoC of a single-SoC board.
		if strings.HasPrefix(qualifiers, "/") && len(socs) == 1 {
			qualifiers = socs[0] + qualifiers
		}
		identifier = name + "/" + qualifiers
	}
	targets, err := boardTargets(dir)
	if err != nil {
		return boardTarget{}, err
	}
	if len(targets) == 0 {
		return boardTarget{}, missingTwisterError(dir, identifier)
	}
	var have []string
	for _, t := range targets {
		if t.Identifier == identifier {
			return t, nil
		}
		have = append(have, t.Identifier)
	}
	sort.Strings(have)
	return boardTarget{}, fmt.Errorf("board %s has no target %s; its targets are: %s", name, identifier, strings.Join(have, ", "))
}

// splitBoardTarget splits name[@revision][/qualifiers] into the board name
// and its qualifiers, dropping the revision, which has no Twister file.
func splitBoardTarget(board string) (name, qualifiers string) {
	board = strings.TrimSpace(board)
	name, qualifiers, _ = strings.Cut(board, "/")
	name, _, _ = strings.Cut(name, "@")
	return name, qualifiers
}

// findBoardDir returns the directory of the board.yml naming board, and the
// board's SoC names. HWMv2 board.yml holds one board or a list of them. Like
// Zephyr's list_boards.py, it fails when more than one board.yml names it.
func findBoardDir(roots []string, board string) (string, []string, error) {
	type match struct {
		dir  string
		socs []string
	}
	var searched []string
	var found []match
	seen := map[string]bool{}
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() || seen[filepath.Clean(root)] {
			continue
		}
		seen[filepath.Clean(root)] = true
		searched = append(searched, root)
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || (d.Name() != "board.yml" && d.Name() != "board.yaml") {
				return nil
			}
			got, ok, err := boardInFile(path, board)
			if err != nil {
				return err
			}
			if ok {
				found = append(found, match{filepath.Dir(path), got})
			}
			return nil
		})
		if err != nil {
			return "", nil, err
		}
	}
	switch len(found) {
	case 0:
		return "", nil, fmt.Errorf("board %s not found (searched: %s)", board, strings.Join(searched, ", "))
	case 1:
		return found[0].dir, found[0].socs, nil
	}
	var dirs []string
	for _, m := range found {
		dirs = append(dirs, m.dir)
	}
	return "", nil, fmt.Errorf("board %s is defined more than once, which west build refuses too: %s", board, strings.Join(dirs, ", "))
}

func boardInFile(path, board string) ([]string, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	if !bytes.Contains(raw, []byte(board)) {
		return nil, false, nil
	}
	type entry struct {
		Name string `yaml:"name"`
		Socs []struct {
			Name string `yaml:"name"`
		} `yaml:"socs"`
	}
	var doc struct {
		Board  *entry  `yaml:"board"`
		Boards []entry `yaml:"boards"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, false, fmt.Errorf("%s: %w", path, err)
	}
	entries := doc.Boards
	if doc.Board != nil {
		entries = append(entries, *doc.Board)
	}
	for _, e := range entries {
		if e.Name != board {
			continue
		}
		var socs []string
		for _, s := range e.Socs {
			socs = append(socs, s.Name)
		}
		return socs, true, nil
	}
	return nil, false, nil
}

// boardTargets reads every Twister file in a board directory: a YAML file
// other than board.yml with an identifier and an arch.
func boardTargets(dir string) ([]boardTarget, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []boardTarget
	for _, e := range entries {
		name := e.Name()
		ext := filepath.Ext(name)
		if e.IsDir() || (ext != ".yaml" && ext != ".yml") || name == "board.yml" || name == "board.yaml" {
			continue
		}
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var doc struct {
			Identifier string `yaml:"identifier"`
			Arch       string `yaml:"arch"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if doc.Identifier == "" || doc.Arch == "" {
			continue
		}
		t := boardTarget{Identifier: doc.Identifier, Arch: doc.Arch, File: path}
		if parts := strings.Split(doc.Identifier, "/"); len(parts) > 1 {
			t.SoC = parts[1]
		}
		out = append(out, t)
	}
	return out, nil
}

// errNoTwister marks a board directory without Twister metadata.
var errNoTwister = errors.New("no Twister YAML")

// missingTwisterError tells the user the one file to add: Zephyr names it
// after the identifier with each / replaced by _.
func missingTwisterError(dir, identifier string) error {
	file := filepath.Join(dir, strings.ReplaceAll(identifier, "/", "_")+".yaml")
	return fmt.Errorf("%w for board %s: windlock reads the board's architecture from it. Add %s containing:\n\n"+
		"identifier: %s\nname: %s\ntype: mcu\narch: arm   # the board's architecture: arm, riscv, x86, xtensa, ...\ntoolchain:\n  - zephyr\n",
		errNoTwister, identifier, file, identifier, identifier)
}

// toolchainsForBoards returns the union of the toolchains each board needs,
// in sorted board order. No boards is an empty list, not an error.
func toolchainsForBoards(roots, boards []string) ([]string, error) {
	sorted := append([]string(nil), boards...)
	sort.Strings(sorted)
	out := []string{}
	for _, board := range sorted {
		t, err := resolveBoard(roots, board)
		if err != nil {
			return nil, err
		}
		tc, err := toolchainForArch(t.Arch)
		if err != nil {
			return nil, fmt.Errorf("board %s: %w", board, err)
		}
		out = unionStrings(out, []string{tc})
	}
	return out, nil
}
