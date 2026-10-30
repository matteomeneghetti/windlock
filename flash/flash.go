// Package flash runs west's flash and debug commands on the host, where the
// probe is on USB, for builds made in the container. It keeps a small west
// workspace per Zephyr commit under <root>/flash: Zephyr's scripts, a
// one-project manifest, and a venv holding the lockfile's Python pins.
package flash

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"windlock/internal/envvar"
	"windlock/lockfile"
)

// Workspace is the host west workspace for one Zephyr commit.
type Workspace struct{ Dir string }

func (w Workspace) zephyr() string { return filepath.Join(w.Dir, "zephyr") }
func (w Workspace) venv() string   { return filepath.Join(w.Dir, ".venv") }

// Dir is <root>/flash, which holds every workspace and the home directory
// the host commands run with.
func Dir(root string) string { return filepath.Join(root, "flash") }

// Steps are what Ensure needs from outside: copying Zephyr's scripts and
// VERSION out of the container's workspace into dst, and syncing a venv.
type Steps struct {
	CopyZephyr func(dst string) error
	SyncVenv   func(venv string, pins []string) error
}

// manifest makes the copied scripts a west workspace that loads Zephyr's
// extension commands, flash and debug among them, and nothing else.
const manifest = `manifest:
  self:
    path: zephyr
    west-commands: scripts/west-commands.yml
`

const westConfig = `[manifest]
path = zephyr
file = west.yml

[zephyr]
base = zephyr
`

// Ensure returns the workspace for entry under root, building it the first
// time and again when the commit's Python pins change.
func Ensure(root string, entry lockfile.Zephyr, steps Steps, log io.Writer) (Workspace, error) {
	if entry.Commit == "" || len(entry.PythonRequirements) == 0 {
		return Workspace{}, fmt.Errorf("zephyr %s has no commit or Python pins; run windlock sync", entry.Name)
	}
	w := Workspace{Dir: filepath.Join(Dir(root), entry.Commit)}
	stamp := entry.Commit + "\n" + entry.PythonInputsSHA256 + "\n" + strings.Join(entry.PythonRequirements, "\n") + "\n"
	ready := filepath.Join(w.Dir, ".ready")
	if raw, err := os.ReadFile(ready); err == nil && string(raw) == stamp {
		return w, nil
	}
	fmt.Fprintf(log, "preparing the host flash workspace for zephyr %s\n", entry.Name)
	if err := os.RemoveAll(w.zephyr()); err != nil {
		return Workspace{}, err
	}
	if err := os.MkdirAll(w.zephyr(), 0o755); err != nil {
		return Workspace{}, err
	}
	if err := steps.CopyZephyr(w.zephyr()); err != nil {
		return Workspace{}, fmt.Errorf("copying Zephyr's scripts from the container: %w", err)
	}
	if _, err := os.Stat(filepath.Join(w.zephyr(), "scripts", "west-commands.yml")); err != nil {
		return Workspace{}, fmt.Errorf("Zephyr's scripts were not copied: %w", err)
	}
	if err := os.WriteFile(filepath.Join(w.zephyr(), "west.yml"), []byte(manifest), 0o644); err != nil {
		return Workspace{}, err
	}
	if err := os.MkdirAll(filepath.Join(w.Dir, ".west"), 0o755); err != nil {
		return Workspace{}, err
	}
	if err := os.WriteFile(filepath.Join(w.Dir, ".west", "config"), []byte(westConfig), 0o644); err != nil {
		return Workspace{}, err
	}
	if err := steps.SyncVenv(w.venv(), entry.PythonRequirements); err != nil {
		return Workspace{}, err
	}
	return w, os.WriteFile(ready, []byte(stamp), 0o644)
}

// Commands are the west commands that run on the host.
var Commands = map[string]bool{"flash": true, "debugserver": true, "rtt": true}

// Request is one west command for the host: the subcommand, its options as
// the user gave them, and the project they are relative to.
type Request struct {
	Project string
	// DefaultBuild is the build directory used when the options name none:
	// the default target's build/<name>.
	DefaultBuild string
	Args         []string // west arguments, the subcommand first
}

// Tools are the host programs the runners use.
type Tools struct {
	Bin string // pinned host-tools bin: python, uv, openocd, dfu-util
	// OpenOCDScripts is the pinned OpenOCD's scripts directory.
	OpenOCDScripts string
	// LookPath finds a program on the user's PATH; tests stand in for it.
	LookPath func(string) (string, error)
}

// vendorTools are runners whose programs windlock cannot install, with the
// program each command needs and where to get it.
var vendorTools = map[string]struct{ flash, debug, from string }{
	"jlink":               {"JLinkExe", "JLinkGDBServerCLExe", "SEGGER's J-Link Software and Documentation Pack, https://www.segger.com/downloads/jlink/"},
	"stm32cubeprogrammer": {"STM32_Programmer_CLI", "", "ST's STM32CubeProgrammer, https://www.st.com/en/development-tools/stm32cubeprog.html"},
}

// Args turns r into the west arguments to run on the host: paths made
// absolute, no rebuild (the container builds), and the runner pointed at the
// pinned tools. It also returns the runner it resolved.
func Args(r Request, t Tools) ([]string, string, error) {
	if len(r.Args) == 0 || !Commands[r.Args[0]] {
		return nil, "", fmt.Errorf("west %s does not run on the host", strings.Join(r.Args, " "))
	}
	sub := r.Args[0]
	opts, rest := splitDashDash(r.Args[1:])
	opts = absPaths(opts, r.Project)
	buildDir := optionValue(opts, "-d", "--build-dir")
	if buildDir == "" {
		if r.DefaultBuild == "" {
			return nil, "", fmt.Errorf("no default target to flash; pass -d build/<target>")
		}
		buildDir = r.DefaultBuild
		opts = append([]string{"-d", buildDir}, opts...)
	}
	if !hasOption(opts, "--rebuild", "--no-rebuild", "--skip-rebuild") {
		opts = append(opts, "--no-rebuild")
	}
	runner := optionValue(opts, "-r", "--runner")
	if runner == "" {
		var err error
		if runner, err = defaultRunner(buildDir, sub); err != nil {
			return nil, "", err
		}
	}
	if err := checkVendor(runner, sub, t); err != nil {
		return nil, "", err
	}
	// west flash goes through every sysbuild domain, and dfu-util would send
	// each one to the same update slot: MCUboot's binary as well as the app's.
	if sub == "flash" && runner == "dfu-util" && !namesDomain(opts) {
		d, err := readDomains(buildDir)
		if err != nil {
			return nil, "", err
		}
		if d.Default != "" {
			opts = append(opts, "--domain", d.Default)
		}
	}
	if runner == "openocd" {
		opts = append(opts, "--openocd", filepath.Join(t.Bin, "openocd"), "--openocd-search", t.OpenOCDScripts)
	}
	args := append([]string{sub}, opts...)
	if rest != nil {
		args = append(append(args, "--"), rest...)
	}
	return args, runner, nil
}

func namesDomain(opts []string) bool {
	for _, o := range opts {
		if o == "--domain" || strings.HasPrefix(o, "--domain=") {
			return true
		}
	}
	return false
}

// VendorProblem says what the build's default flash runner is missing, if it
// is a vendor tool. A build that does not exist yet has no default runner.
func VendorProblem(buildDir string, t Tools) (runner string, err error) {
	runner, err = defaultRunner(buildDir, "flash")
	if err != nil {
		return "", nil
	}
	return runner, checkVendor(runner, "flash", t)
}

func checkVendor(runner, sub string, t Tools) error {
	v, ok := vendorTools[runner]
	if !ok {
		return nil
	}
	prog := v.flash
	if sub != "flash" && v.debug != "" {
		prog = v.debug
	}
	if _, err := t.LookPath(prog); err != nil {
		return fmt.Errorf("the %s runner needs %s, which windlock cannot install for you: get %s. "+
			"Or flash with the tools windlock installs: -r openocd (ST-Link, J-Link, CMSIS-DAP) or -r dfu-util", runner, prog, v.from)
	}
	return nil
}

// defaultRunner reads the default sysbuild domain's runners.yaml, or the
// build's own.
func defaultRunner(buildDir, sub string) (string, error) {
	dir, err := DomainDir(buildDir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "zephyr", "runners.yaml")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%s has no runners.yaml; build it first with windlock build", buildDir)
	}
	if err != nil {
		return "", err
	}
	var runners struct {
		Flash string `yaml:"flash-runner"`
		Debug string `yaml:"debug-runner"`
	}
	if err := yaml.Unmarshal(raw, &runners); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	runner := runners.Debug
	if sub == "flash" {
		runner = runners.Flash
	}
	if runner == "" {
		return "", fmt.Errorf("%s names no %s runner; pass -r <runner>", path, sub)
	}
	return runner, nil
}

// BuildDir is the build directory args name, or def.
func BuildDir(args []string, project, def string) string {
	opts, _ := splitDashDash(args)
	if d := optionValue(absPaths(opts, project), "-d", "--build-dir"); d != "" {
		return d
	}
	return def
}

// OptionValue is the value args give the short or long option, or "".
func OptionValue(args []string, short, long string) string {
	opts, _ := splitDashDash(args)
	return optionValue(opts, short, long)
}

// pathOptions take a path, which the host command resolves from its own
// workspace, so relative ones are made absolute against the project.
var pathOptions = map[string]bool{
	"-d": true, "--build-dir": true, "-f": true, "--file": true,
	"--elf-file": true, "--hex-file": true, "--bin-file": true,
}

func absPaths(opts []string, project string) []string {
	out := append([]string(nil), opts...)
	abs := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(project, p)
	}
	for i := 0; i < len(out); i++ {
		o := out[i]
		if name, value, ok := strings.Cut(o, "="); ok && strings.HasPrefix(o, "--") && pathOptions[name] {
			out[i] = name + "=" + abs(value)
			continue
		}
		if pathOptions[o] && i+1 < len(out) {
			out[i+1] = abs(out[i+1])
			i++
			continue
		}
		if len(o) > 2 && (strings.HasPrefix(o, "-d") || strings.HasPrefix(o, "-f")) && o[1] != '-' {
			out[i] = o[:2] + abs(o[2:])
		}
	}
	return out
}

// optionValue is the value of the short or long option in opts, or "".
func optionValue(opts []string, short, long string) string {
	for i, o := range opts {
		switch {
		case (o == short || o == long) && i+1 < len(opts):
			return opts[i+1]
		case strings.HasPrefix(o, long+"="):
			return strings.TrimPrefix(o, long+"=")
		case len(o) > len(short) && strings.HasPrefix(o, short) && !strings.HasPrefix(o, "--"):
			return o[len(short):]
		}
	}
	return ""
}

func hasOption(opts []string, names ...string) bool {
	for _, o := range opts {
		for _, n := range names {
			if o == n {
				return true
			}
		}
	}
	return false
}

func splitDashDash(args []string) (opts, rest []string) {
	for i, a := range args {
		if a == "--" {
			return args[:i], append([]string{}, args[i+1:]...)
		}
	}
	return args, nil
}

// Env is the environment for west on the host: the pinned tools and the
// venv first on PATH, and a home under <root>/flash, so what probe tools
// write (J-Link settings, pyOCD's packs, history) stays in the root.
func Env(w Workspace, root string, t Tools, base []string) []string {
	home := filepath.Join(Dir(root), "home")
	path := t.Bin + string(os.PathListSeparator) + filepath.Join(w.venv(), "bin")
	if cur := envvar.Get(base, "PATH"); cur != "" {
		path += string(os.PathListSeparator) + cur
	}
	env := envvar.Set(base, "PATH", path)
	env = envvar.Set(env, "ZEPHYR_BASE", w.zephyr())
	env = envvar.Set(env, "VIRTUAL_ENV", w.venv())
	env = envvar.Set(env, "PYTHONNOUSERSITE", "1")
	env = envvar.Set(env, "HOME", home)
	for k, sub := range map[string]string{"XDG_CONFIG_HOME": ".config", "XDG_CACHE_HOME": ".cache", "XDG_DATA_HOME": ".local/share", "XDG_STATE_HOME": ".local/state"} {
		env = envvar.Set(env, k, filepath.Join(home, sub))
	}
	return envvar.Unset(env, "ZEPHYR_SDK_INSTALL_DIR")
}

// Command is west with args on the host, in w.
func Command(w Workspace, root string, t Tools, args []string) (*exec.Cmd, error) {
	if err := os.MkdirAll(filepath.Join(Dir(root), "home"), 0o755); err != nil {
		return nil, err
	}
	cmd := exec.Command(filepath.Join(w.venv(), "bin", "west"), args...)
	cmd.Dir = w.Dir
	cmd.Env = Env(w, root, t, os.Environ())
	return cmd, nil
}
