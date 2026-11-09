package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/matteomeneghetti/windlock/container"
	"github.com/matteomeneghetti/windlock/flash"
	"github.com/matteomeneghetti/windlock/invoke"
	"github.com/matteomeneghetti/windlock/lockfile"
	"github.com/matteomeneghetti/windlock/setup"
	"github.com/matteomeneghetti/windlock/tools"
)

// hostWest are the west commands that run on the host, where the probe is
// on USB; debug and attach also run gdb in the container.
var hostWest = map[string]bool{"flash": true, "debugserver": true, "rtt": true, "debug": true, "attach": true}

// hostFlash is what a host west command needs: the workspace, the pinned
// tools, and the project.
type hostFlash struct {
	root, project, defaultBuild string
	ws                          flash.Workspace
	tools                       flash.Tools
}

// The steps that reach outside the process; tests stand in for them.
var (
	installFlash = tools.InstallFlash
	copyZephyr   = func(project string, entry lockfile.Zephyr, dst string) error {
		root, err := tools.Root()
		if err != nil {
			return err
		}
		engine, err := container.FindEngine()
		if err != nil {
			return err
		}
		triple, err := container.Triple(runtime.GOARCH)
		if err != nil {
			return err
		}
		ws, err := setup.WorkspaceDir(container.RootPath, project, entry.Commit)
		if err != nil {
			return err
		}
		return container.CopyOut(engine, projectImage(project), "linux/"+runtime.GOARCH,
			container.VolumeName(root, triple), filepath.Join(ws, "zephyr"), dst, "scripts", "VERSION")
	}
	syncVenv = setup.SyncVenv
)

// prepareHostFlash installs the flash tools and the workspace for the
// project's default Zephyr, holding the root lock while it writes.
func prepareHostFlash(project string, s streams) (hostFlash, error) {
	root, err := tools.Root()
	if err != nil {
		return hostFlash{}, err
	}
	lock, err := lockfile.ReadFile(filepath.Join(project, lockfile.Filename))
	if err != nil {
		return hostFlash{}, fmt.Errorf("%w; run windlock init first", err)
	}
	entry, err := lock.DefaultEntry()
	if err != nil {
		return hostFlash{}, err
	}
	release, err := holdRootLock(root, s.errOut)
	if err != nil {
		return hostFlash{}, err
	}
	defer release()
	// Only show the install log when it installed something or failed.
	var log bytes.Buffer
	bin, err := installFlash(&log)
	if err != nil || bytes.Contains(log.Bytes(), []byte(": installing")) || s.verbose {
		s.errOut.Write(log.Bytes())
	}
	if err != nil {
		return hostFlash{}, err
	}
	ws, err := flash.Ensure(root, entry, flash.Steps{
		CopyZephyr: func(dst string) error { return copyZephyr(project, entry, dst) },
		SyncVenv: func(venv string, pins []string) error {
			return syncVenv(bin, root, venv, pins, s.errOut)
		},
	}, s.errOut)
	if err != nil {
		return hostFlash{}, err
	}
	h := hostFlash{
		root: root, project: project, ws: ws,
		tools: flash.Tools{
			Bin:            bin,
			OpenOCDScripts: filepath.Join(filepath.Dir(bin), "openocd", "scripts"),
			LookPath:       exec.LookPath,
		},
	}
	if cfg, err := lockfile.ReadConfigFile(filepath.Join(project, lockfile.ConfigFilename)); err == nil {
		if t, err := cfg.TargetNamed(""); err == nil {
			h.defaultBuild = filepath.Join(project, "build", t.Name)
		}
	}
	return h, nil
}

// runHostWest runs west flash, debugserver or rtt on the host.
func runHostWest(project string, args []string, s streams) error {
	global, rest := splitWestGlobal(args)
	h, err := prepareHostFlash(project, s)
	if err != nil {
		return err
	}
	westArgs, _, err := flash.Args(flash.Request{Project: project, DefaultBuild: h.defaultBuild, Args: rest}, h.tools)
	if err != nil {
		return err
	}
	cmd, err := flash.Command(h.ws, h.root, h.tools, append(global, westArgs...))
	if err != nil {
		return err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = s.in, s.out, s.errOut
	return exitError(cmd.Run())
}

// runHostDebug runs the runner's debug server on the host and the build's
// gdb in the container, connected over TCP; attach skips loading the image.
func runHostDebug(project string, args []string, s streams) error {
	global, rest := splitWestGlobal(args)
	sub, opts := rest[0], rest[1:]
	h, err := prepareHostFlash(project, s)
	if err != nil {
		return err
	}
	westArgs, runner, err := flash.Args(flash.Request{Project: project, DefaultBuild: h.defaultBuild, Args: append([]string{"debugserver"}, opts...)}, h.tools)
	if err != nil {
		return err
	}
	debug, ok := flash.DebugRunners[runner]
	if !ok {
		return fmt.Errorf("runner %s has no debug server windlock can reach from the container; use -r openocd, -r jlink or -r pyocd", runner)
	}
	port := flash.OptionValue(opts, "--gdb-port", "--gdb-port")
	if port == "" {
		port = debug.Port
	}
	// The runner's debugserver resets the target unless told otherwise.
	if sub == "attach" && runner == "openocd" {
		westArgs = append(westArgs[:1:1], append([]string{"--cmd-reset-halt", "halt"}, westArgs[1:]...)...)
	}
	domain, err := flash.DomainDir(flash.BuildDir(westArgs[1:], project, h.defaultBuild))
	if err != nil {
		return err
	}
	cmd, err := flash.Command(h.ws, h.root, h.tools, append(global, westArgs...))
	if err != nil {
		return err
	}
	server, err := startDebugServer(cmd, debug.Ready, s.verbose, s.errOut)
	if err != nil {
		return err
	}
	defer server.stop()
	defer server.stopOnSignal()()
	if err := server.waitReady(30 * time.Second); err != nil {
		return err
	}
	inner := []string{"gdb-remote", "--runner", runner, "--remote", "host.docker.internal:" + port, "--build-dir", domain}
	if sub == "debug" {
		inner = append(inner, "--load")
	}
	return runInContainer(project, inner, false, s)
}

// splitWestGlobal splits west's own options from the subcommand and its
// arguments.
func splitWestGlobal(args []string) (global, rest []string) {
	for i, a := range args {
		if !strings.HasPrefix(a, "-") {
			return args[:i], args[i:]
		}
	}
	return args, nil
}

func exitError(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return &invoke.ExitError{Code: exit.ExitCode()}
	}
	return err
}

// cmdGDBRemote runs in the container: the build's gdb, connected to the
// debug server the host started.
func cmdGDBRemote(args []string, s streams) error {
	if !container.Inside() {
		return fmt.Errorf("gdb-remote runs inside the container; use windlock west debug")
	}
	fs := newFlags("gdb-remote", s)
	runner := fs.String("runner", "", "")
	remote := fs.String("remote", "", "")
	buildDir := fs.String("build-dir", "", "")
	load := fs.Bool("load", false, "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *runner == "" || *remote == "" || *buildDir == "" {
		return errUsage
	}
	gdb, gdbArgs, err := flash.GDBArgs(*buildDir, *runner, *remote, *load)
	if err != nil {
		return err
	}
	cmd := exec.Command(gdb, gdbArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = s.in, s.out, s.errOut
	return exitError(cmd.Run())
}

// cmdProbe makes flash, debug and attach: [target] [--no-build] [-r runner]
// [-- west options]. It builds the target in the container first (attach
// does not), then runs west on this machine against build/<target>.
func cmdProbe(sub string) func([]string, streams) error {
	return func(args []string, s streams) error {
		if container.Inside() {
			return fmt.Errorf("%s runs on the host, where the probe is", sub)
		}
		name, rest := "", args
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			name, rest = args[0], args[1:]
		}
		ours, passthrough := rest, []string(nil)
		for i, a := range rest {
			if a == "--" {
				ours, passthrough = rest[:i], rest[i+1:]
				break
			}
		}
		fs := newFlags(sub, s)
		noBuild := fs.Bool("no-build", sub == "attach", "do not build the target first")
		var runner string
		fs.StringVar(&runner, "r", "", "west runner, e.g. openocd, jlink, dfu-util")
		fs.StringVar(&runner, "runner", "", "west runner")
		if err := fs.Parse(ours); err != nil || fs.NArg() != 0 || name == "" && len(args) > 0 && args[0] == "" {
			return errUsage
		}
		project, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := lockfile.ReadConfigFile(filepath.Join(project, lockfile.ConfigFilename))
		if err != nil {
			return fmt.Errorf("%w; run windlock init first", err)
		}
		target, err := cfg.TargetNamed(name)
		if err != nil {
			return err
		}
		if !*noBuild {
			if err := runInContainer(project, []string{"build", target.Name}, false, s); err != nil {
				return err
			}
		}
		west := []string{sub, "-d", filepath.Join(project, "build", target.Name)}
		if runner != "" {
			west = append(west, "-r", runner)
		}
		west = append(west, passthrough...)
		if sub == "flash" {
			return runHostWest(project, west, s)
		}
		return runHostDebug(project, west, s)
	}
}
