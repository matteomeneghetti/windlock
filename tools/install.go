// Package tools installs the pinned host tools: CMake, Ninja, CPython, dtc, gperf and uv.
package tools

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"windlock/host"
)

// Install downloads, verifies and installs the pinned host tools under the
// toolchain root; nothing is written outside it. The flash tools are left
// to InstallFlash.
func Install(log io.Writer) error {
	_, err := install(log, func(a Archive) bool { return !a.flashOnly() })
	return err
}

// install installs the pins for this host that want selects into the
// host-tools directory, and returns its bin.
func install(log io.Writer, want func(Archive) bool) (string, error) {
	triple, err := host.Current()
	if err != nil {
		return "", err
	}
	pins, err := HostPins(triple)
	if err != nil {
		return "", err
	}
	root, err := Root()
	if err != nil {
		return "", err
	}
	id := ID(versionsOf(pins))
	hostDir := filepath.Join(root, "host", id)
	bin := filepath.Join(hostDir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return "", err
	}
	fmt.Fprintf(log, "host %s\n", triple)
	fmt.Fprintf(log, "prefix %s\n", hostDir)
	var names []string
	for _, a := range pins {
		if !want(a) {
			continue
		}
		if err := installOne(root, hostDir, bin, a, log); err != nil {
			return "", err
		}
		names = append(names, binName(a))
	}
	if err := chmodBins(bin); err != nil {
		return "", err
	}
	return bin, checkFloors(bin, names, log)
}

func installOne(root, hostDir, bin string, a Archive, log io.Writer) error {
	dest := filepath.Join(bin, binName(a))
	marker := filepath.Join(hostDir, ".installed", a.Name)
	if installed(marker, dest, a.SHA256) {
		fmt.Fprintf(log, "%s: already installed\n", a.Name)
		return nil
	}
	arc, err := installArchive(root, a, log)
	if err != nil {
		return fmt.Errorf("%s: %w", a.Name, err)
	}
	fmt.Fprintf(log, "%s: installing\n", a.Name)
	switch a.Kind {
	case kindCMake:
		if err := installPrefix(root, hostDir, arc, a.SHA256, "cmake"); err != nil {
			return fmt.Errorf("%s: %w", a.Name, err)
		}
	case kindPython:
		if err := installPrefix(root, hostDir, arc, a.SHA256, "python3"); err != nil {
			return fmt.Errorf("%s: %w", a.Name, err)
		}
	case kindNinja:
		if err := extractNinja(arc, bin); err != nil {
			return fmt.Errorf("%s: %w", a.Name, err)
		}
	case kindUV:
		if err := installUV(root, arc, a.SHA256, bin); err != nil {
			return fmt.Errorf("%s: %w", a.Name, err)
		}
	case kindOpenOCD:
		if err := installPrefix(root, hostDir, arc, a.SHA256, "openocd"); err != nil {
			return fmt.Errorf("%s: %w", a.Name, err)
		}
	case kindDFUUtil:
		if err := buildDFUUtil(root, arc, a.SrcDir, hostDir, log); err != nil {
			return fmt.Errorf("%s: %w", a.Name, err)
		}
	case kindGperf:
		if err := buildAutotools(root, arc, a.SrcDir, hostDir, nil, log); err != nil {
			return fmt.Errorf("%s: %w", a.Name, err)
		}
	case kindDTC:
		if err := buildDTC(root, arc, a.SrcDir, bin, log); err != nil {
			return fmt.Errorf("%s: %w", a.Name, err)
		}
	default:
		return fmt.Errorf("%s: unsupported archive", a.Name)
	}
	if _, err := os.Stat(dest); err != nil {
		return fmt.Errorf("%s: %s was not installed", a.Name, dest)
	}
	return copyToMode(strings.NewReader(a.SHA256+"\n"), marker, 0o644)
}

// installed reports whether a finished install of the archive with sum is
// in place. The marker is written last, so an interrupted install has none
// and is redone over whatever it left.
func installed(marker, dest, sum string) bool {
	got, err := os.ReadFile(marker)
	if err != nil || strings.TrimSpace(string(got)) != sum {
		return false
	}
	info, err := os.Stat(dest)
	return err == nil && !info.IsDir()
}

// installPrefix merges the directory holding bin/<name> into hostDir: one level
// down in Linux archives, CMake.app/Contents in the macOS CMake one.
func installPrefix(root, hostDir, archive, sum, name string) error {
	stage, err := stageExtract(root, sum, archive, 0)
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	prefix, err := prefixDir(stage, name)
	if err != nil {
		return err
	}
	return mergeTree(prefix, hostDir)
}

// installUV copies uv from its archive's single top directory into bin.
func installUV(root, archive, sum, bin string) error {
	stage, err := stageExtract(root, sum, archive, 1)
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	return copyFile(filepath.Join(stage, "uv"), filepath.Join(bin, "uv"), 0o755)
}

// prefixDir finds bin/<name> and returns the directory that contains bin.
func prefixDir(stage, name string) (string, error) {
	var found string
	err := filepath.Walk(stage, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Base(p) != name || filepath.Base(filepath.Dir(p)) != "bin" {
			return nil
		}
		if found != "" {
			return fmt.Errorf("more than one bin/%s", name)
		}
		found = filepath.Dir(filepath.Dir(p))
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("archive has no bin/%s", name)
	}
	return found, nil
}

func binName(a Archive) string {
	if a.Name == "python" {
		return "python3"
	}
	return a.Name
}

func chmodBins(bin string) error {
	for _, name := range BinNames {
		p := filepath.Join(bin, name)
		if err := os.Chmod(p, 0o755); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// checkFloors runs each named tool in bin and checks the versions Zephyr needs.
func checkFloors(bin string, names []string, log io.Writer) error {
	checks := map[string]func(text string) error{
		"cmake":   func(text string) error { return requireAtLeast("cmake", text, cmakeFloor) },
		"python3": requirePython312,
		"dtc":     func(text string) error { return requireAtLeast("dtc", text, dtcFloor) },
	}
	for _, name := range names {
		text, err := versionText(filepath.Join(bin, name))
		if err != nil {
			return err
		}
		if check := checks[name]; check != nil {
			if err := check(text); err != nil {
				return err
			}
		}
		fmt.Fprintf(log, "%s: %s\n", name, firstLine(text))
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
