// Package tools installs the tools a lockfile pins: the build tools the
// container uses (CMake, Ninja, CPython, uv, dtc, gperf) and the flash tools
// a host with the probe uses (OpenOCD, dfu-util, with CPython and uv).
package tools

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Install downloads, verifies and installs the tools in s under the
// toolchain root, and returns their bin directory. Nothing is written
// outside the root.
func Install(s Set, log io.Writer) (string, error) {
	if err := Validate(s); err != nil {
		return "", err
	}
	root, err := Root()
	if err != nil {
		return "", err
	}
	hostDir := filepath.Join(root, "host", ID(s))
	bin := filepath.Join(hostDir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return "", err
	}
	fmt.Fprintf(log, "prefix %s\n", hostDir)
	var names []string
	for _, t := range s.Tools {
		if err := installOne(root, hostDir, bin, s, t, log); err != nil {
			return "", err
		}
		names = append(names, binName(t.Name))
	}
	if err := chmodBins(bin); err != nil {
		return "", err
	}
	return bin, checkFloors(bin, names, log)
}

// BinDirFor is the bin directory Install uses for s under root.
func BinDirFor(root string, s Set) string { return BinDir(root, ID(s)) }

func installOne(root, hostDir, bin string, s Set, t Tool, log io.Writer) error {
	dest := filepath.Join(bin, binName(t.Name))
	marker := filepath.Join(hostDir, ".installed", t.Name)
	if installed(marker, dest, t.SHA256) {
		fmt.Fprintf(log, "%s: already installed\n", t.Name)
		return nil
	}
	arc, err := installArchive(root, t, log)
	if err != nil {
		return fmt.Errorf("%s: %w", t.Name, err)
	}
	fmt.Fprintf(log, "%s: installing\n", t.Name)
	r := toolRecipes[t.Name]
	built := false
	switch r.kind {
	case kindCMake:
		err = installPrefix(root, hostDir, arc, t.SHA256, "cmake")
	case kindPython:
		err = installPrefix(root, hostDir, arc, t.SHA256, "python3")
	case kindNinja:
		err = extractNinja(arc, bin)
	case kindUV:
		err = installUV(root, arc, t.SHA256, bin)
	case kindOpenOCD:
		err = installPrefix(root, hostDir, arc, t.SHA256, "openocd")
	case kindDFUUtil:
		built = true
		err = buildDFUUtil(root, s, arc, t.SHA256, hostDir, log)
	case kindGperf:
		built = true
		err = buildFromSource(root, arc, t.SHA256, hostDir, nil, nil, log)
	case kindDTC:
		built = true
		err = buildDTC(root, s, arc, t.SHA256, bin, log)
	default:
		err = fmt.Errorf("unsupported archive")
	}
	if err != nil {
		return fmt.Errorf("%s: %w", t.Name, err)
	}
	if _, err := os.Stat(dest); err != nil {
		return fmt.Errorf("%s: %s was not installed", t.Name, dest)
	}
	if built {
		recordCompiler(root, filepath.Join(hostDir, ".installed", t.Name+".cc"))
	}
	return copyToMode(strings.NewReader(t.SHA256+"\n"), marker, 0o644)
}

// BuiltWith returns, for each tool in s that windlock built from source
// under root, the first line of the C compiler's --version it was built
// with. Container builds use the image's compiler; a Mac's comes from the
// Xcode command line tools, which windlock does not pin.
func BuiltWith(root string, s Set) map[string]string {
	out := map[string]string{}
	dir := filepath.Join(root, "host", ID(s), ".installed")
	for _, t := range s.Tools {
		if raw, err := os.ReadFile(filepath.Join(dir, t.Name+".cc")); err == nil {
			out[t.Name] = strings.TrimSpace(string(raw))
		}
	}
	return out
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

func binName(name string) string {
	if name == "python" {
		return "python3"
	}
	return name
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
