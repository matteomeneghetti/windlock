package flash

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DebugServer describes a runner's debug server.
type DebugServer struct {
	Port  string // the port it listens on by default
	Ready string // text in its output once it listens
}

// DebugRunners are the runners whose debug server gdb can reach over TCP.
var DebugRunners = map[string]DebugServer{
	"openocd": {Port: "3333", Ready: "for gdb connections"},
	"jlink":   {Port: "2331", Ready: "Waiting for GDB connection"},
	"pyocd":   {Port: "3333", Ready: "GDB server"},
}

// domains is a sysbuild build's domains.yaml; a build without sysbuild has
// none, and gives the zero value.
type domains struct {
	Default string `yaml:"default"`
	Domains []struct {
		Name     string `yaml:"name"`
		BuildDir string `yaml:"build_dir"`
	} `yaml:"domains"`
}

func readDomains(buildDir string) (domains, error) {
	var d domains
	raw, err := os.ReadFile(filepath.Join(buildDir, "domains.yaml"))
	if os.IsNotExist(err) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	if err := yaml.Unmarshal(raw, &d); err != nil {
		return d, fmt.Errorf("%s: %w", filepath.Join(buildDir, "domains.yaml"), err)
	}
	return d, nil
}

// DomainDir is the build directory of buildDir's default sysbuild domain,
// or buildDir itself without sysbuild.
func DomainDir(buildDir string) (string, error) {
	d, err := readDomains(buildDir)
	if err != nil {
		return "", err
	}
	if d.Default == "" {
		return buildDir, nil
	}
	for _, domain := range d.Domains {
		if domain.Name == d.Default {
			return domain.BuildDir, nil
		}
	}
	return "", fmt.Errorf("%s names no build directory for its default domain %q", filepath.Join(buildDir, "domains.yaml"), d.Default)
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
	// The server may still be resetting the target when gdb connects.
	ex := []string{"set remotetimeout 10"}
	switch runner {
	case "jlink":
		ex = append(ex, "target remote "+remote)
		if load {
			ex = append(ex, "monitor halt", "monitor reset", "load")
		}
	case "openocd", "pyocd":
		ex = append(ex, "target extended-remote "+remote)
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
