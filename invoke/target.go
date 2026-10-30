package invoke

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"windlock/internal/envvar"
	"windlock/lockfile"
	"windlock/setup"
)

// Command is one west invocation.
type Command struct {
	West string
	Args []string
	Dir  string
	Env  []string
	// Workspace is the west topdir whose .west/config build uses.
	Workspace string
}

// workspaceEnv is the isolated environment of the default Zephyr entry: its
// venv's west, run in appDir, with the pinned tools, tree and SDK.
func workspaceEnv(lock lockfile.Lockfile, root, triple, appDir, existingPATH string) (Command, error) {
	entry, err := lock.DefaultEntry()
	if err != nil {
		return Command{}, err
	}
	platform, err := lock.Platform(triple)
	if err != nil {
		return Command{}, err
	}
	if _, ok := platform.SDK[entry.SDK]; !ok {
		return Command{}, fmt.Errorf("no SDK %s bundle for %s", entry.SDK, triple)
	}
	ws, err := setup.WorkspaceDir(root, appDir, entry.Commit)
	if err != nil {
		return Command{}, err
	}
	zephyrBase := filepath.Join(ws, "zephyr")
	venv := filepath.Join(ws, ".venv")
	hostBin, err := setup.HostToolsBin(lock, root, triple)
	if err != nil {
		return Command{}, err
	}
	sdkDir := filepath.Join(root, "sdk", entry.SDK, triple, "zephyr-sdk-"+entry.SDK)
	if err := requireDir(zephyrBase); err != nil {
		return Command{}, fmt.Errorf("zephyr workspace: %w; run windlock sync", err)
	}
	if err := requireDir(filepath.Join(venv, "bin")); err != nil {
		return Command{}, fmt.Errorf("workspace venv: %w; run windlock sync", err)
	}
	if err := requireDir(hostBin); err != nil {
		return Command{}, fmt.Errorf("host tools: %w; run windlock sync", err)
	}
	if !setup.SDKInstalled(sdkDir, entry.SDK) {
		return Command{}, fmt.Errorf("SDK %s is not installed at %s; run windlock sync", entry.SDK, sdkDir)
	}
	return Command{
		West:      filepath.Join(venv, "bin", "west"),
		Dir:       appDir,
		Env:       childEnv(existingPATH, root, hostBin, venv, zephyrBase, sdkDir),
		Workspace: ws,
	}, nil
}

// buildCommand is west build for a target into build/<name>, with extra west
// options and, after --, CMake arguments; -b, -d and -s belong to the target.
func buildCommand(cfg lockfile.Config, lock lockfile.Lockfile, root, triple, appDir, existingPATH, targetName string, extra []string) (Command, error) {
	target, err := cfg.TargetNamed(targetName)
	if err != nil {
		return Command{}, err
	}
	westOpts, cmakeArgs := splitDashDash(extra)
	if err := rejectOwned(target.Name, westOpts); err != nil {
		return Command{}, err
	}
	cmd, err := workspaceEnv(lock, root, triple, appDir, existingPATH)
	if err != nil {
		return Command{}, err
	}
	if err := requireSource(appDir, target); err != nil {
		return Command{}, err
	}
	cmd.Args = westArgs(target, westOpts, cmakeArgs)
	return cmd, nil
}

func westArgs(target lockfile.Target, westOpts, cmakeArgs []string) []string {
	args := []string{"build", "-b", target.Board, "-d", path.Join("build", target.Name)}
	// --sysbuild and --no-sysbuild are one west option, so a choice in the
	// forwarded options wins over the target's.
	if target.Sysbuild && !hasSysbuildChoice(westOpts) {
		args = append(args, "--sysbuild")
	}
	args = append(args, westOpts...)
	args = append(args, target.Source)
	if cmakeArgs != nil {
		args = append(append(args, "--"), cmakeArgs...)
	}
	return args
}

func hasSysbuildChoice(opts []string) bool {
	for _, o := range opts {
		if o == "--sysbuild" || o == "--no-sysbuild" {
			return true
		}
	}
	return false
}

// splitDashDash splits args at the first --. cmake is nil when there is no
// --, and empty when -- ends the list.
func splitDashDash(args []string) (west, cmake []string) {
	for i, a := range args {
		if a == "--" {
			return args[:i], append([]string{}, args[i+1:]...)
		}
	}
	return args, nil
}

// rejectOwned refuses west build options that would override the target:
// -b/--board, -d/--build-dir and -s/--source-dir, in every spelling.
func rejectOwned(target string, opts []string) error {
	for _, o := range opts {
		for _, owned := range []struct{ short, long string }{{"-b", "--board"}, {"-d", "--build-dir"}, {"-s", "--source-dir"}} {
			if o == owned.short || o == owned.long || strings.HasPrefix(o, owned.long+"=") ||
				(strings.HasPrefix(o, owned.short) && !strings.HasPrefix(o, "--")) {
				return fmt.Errorf("%s is set by target %s; change it with windlock target remove and target add", owned.long, target)
			}
		}
	}
	return nil
}

func requireSource(appDir string, target lockfile.Target) error {
	dir := target.Source
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(appDir, dir)
	}
	if err := requireDir(dir); err != nil {
		return fmt.Errorf("target %q source: %w", target.Name, err)
	}
	return nil
}

func childEnv(existingPATH, root, hostBin, venv, zephyrBase, sdkDir string) []string {
	venvBin := filepath.Join(venv, "bin")
	path := venvBin + string(os.PathListSeparator) + hostBin
	if existingPATH != "" {
		path += string(os.PathListSeparator) + existingPATH
	}
	env := setup.Isolate(os.Environ(), root)
	env = envvar.Set(env, "PATH", path)
	env = envvar.Set(env, "ZEPHYR_TOOLCHAIN_VARIANT", "zephyr")
	env = envvar.Set(env, "ZEPHYR_SDK_INSTALL_DIR", sdkDir)
	env = envvar.Set(env, "ZEPHYR_BASE", zephyrBase)
	env = envvar.Set(env, "VIRTUAL_ENV", venv)
	return env
}

func requireDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}
