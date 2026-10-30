package flash

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DebugRunners are the runners whose debug server gdb can reach over TCP,
// with the port each listens on by default.
var DebugRunners = map[string]string{"openocd": "3333", "jlink": "2331", "pyocd": "3333"}

// DomainDir is the build directory of buildDir's default sysbuild domain,
// or buildDir itself without sysbuild.
func DomainDir(buildDir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(buildDir, "domains.yaml"))
	if os.IsNotExist(err) {
		return buildDir, nil
	}
	if err != nil {
		return "", err
	}
	var domains struct {
		Default string `yaml:"default"`
		Domains []struct {
			Name     string `yaml:"name"`
			BuildDir string `yaml:"build_dir"`
		} `yaml:"domains"`
	}
	if err := yaml.Unmarshal(raw, &domains); err != nil {
		return "", fmt.Errorf("%s: %w", filepath.Join(buildDir, "domains.yaml"), err)
	}
	for _, d := range domains.Domains {
		if d.Name == domains.Default {
			return d.BuildDir, nil
		}
	}
	return "", fmt.Errorf("%s names no build directory for its default domain %q", filepath.Join(buildDir, "domains.yaml"), domains.Default)
}

// GDBArgs are the arguments for the build's gdb to debug (load the image
// first) or attach to a target behind the debug server at remote, as the
// runner's own debug command would. It runs in the container, where the
// runners.yaml gdb path exists.
func GDBArgs(domainDir, runner, remote string, load bool) (gdb string, args []string, err error) {
	path := filepath.Join(domainDir, "zephyr", "runners.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	var doc struct {
		Config struct {
			GDB     string `yaml:"gdb"`
			ELFFile string `yaml:"elf_file"`
		} `yaml:"config"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return "", nil, fmt.Errorf("%s: %w", path, err)
	}
	if doc.Config.GDB == "" || doc.Config.ELFFile == "" {
		return "", nil, fmt.Errorf("%s names no gdb or elf_file", path)
	}
	elf := doc.Config.ELFFile
	if !filepath.IsAbs(elf) {
		elf = filepath.Join(domainDir, "zephyr", elf)
	}
	var ex []string
	switch runner {
	case "jlink":
		ex = []string{"target remote " + remote}
		if load {
			ex = append(ex, "monitor halt", "monitor reset", "load")
		}
	case "openocd", "pyocd":
		ex = []string{"target extended-remote " + remote}
		if load {
			ex = append(ex, "monitor reset halt", "load")
		}
	default:
		return "", nil, fmt.Errorf("runner %s has no debug server windlock can reach; use openocd, jlink or pyocd", runner)
	}
	for _, c := range ex {
		args = append(args, "-ex", c)
	}
	return doc.Config.GDB, append(args, elf), nil
}
