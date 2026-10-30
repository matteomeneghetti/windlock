package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"windlock/container"
	"windlock/internal/linuxbin"
	"windlock/internal/rootlock"
	"windlock/internal/tty"
	"windlock/invoke"
	"windlock/lockfile"
	"windlock/setup"
	"windlock/tools"
)

// routed are the commands that run inside the container. Everything that
// builds runs there; the host runs only the launcher and the probe side.
var routed = map[string]bool{
	"init": true, "sync": true, "build": true, "west": true, "shell": true, "target": true, "zephyr": true,
}

// dispatch runs a command: in the container when it builds, unless this
// process already is the container's windlock.
func dispatch(name string, cmd func([]string, streams) error, args []string, s streams) error {
	if !routed[name] || container.Inside() {
		return cmd(args, s)
	}
	// The command checks its own arguments, then stops before doing anything.
	check := s
	check.routing = true
	if err := cmd(args, check); !errors.Is(err, errRouted) {
		return err
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	// The probe is on the host's USB, so these run on the host.
	if sub := invoke.WestSubcommand(args); name == "west" && hostWest[sub] {
		if sub == "debug" || sub == "attach" {
			return runHostDebug(wd, args, s)
		}
		return runHostWest(wd, args, s)
	}
	return runInContainer(wd, append([]string{name}, args...), mutates(name, args), s)
}

// checkContainerMounts fails in the container when the engine did not
// share the toolchain root or the project with it.
func checkContainerMounts() error {
	if !container.Inside() {
		return nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	return container.CheckMounts(wd)
}

// linuxBinary finds the Linux windlock, and ensureVolume prepares the
// container's toolchain root; tests stand in for both.
var (
	linuxBinary  = linuxbin.Path
	ensureVolume = container.EnsureVolume
)

// mutates reports whether a command writes the toolchain root, so it must
// hold the root's lock.
func mutates(name string, args []string) bool {
	switch name {
	case "init", "sync":
		return true
	case "target", "zephyr":
		return len(args) > 0 && (args[0] == "add" || args[0] == "remove")
	}
	return false
}

// runInContainer runs windlock with args in the container, in project.
func runInContainer(project string, args []string, lock bool, s streams) error {
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
	binary, err := linuxBinary(filepath.Join(root, "container", "bin"))
	if err != nil {
		return err
	}
	if lock {
		l, err := rootlock.Acquire(root, s.errOut)
		if err != nil {
			return err
		}
		defer l.Release()
	}
	installs := args[0] == "init" || args[0] == "sync"
	if s.verbose {
		args = append([]string{"-v"}, args...)
	}
	spec := container.Spec{
		Engine:         engine,
		Image:          projectImage(project),
		Platform:       "linux/" + runtime.GOARCH,
		Volume:         container.VolumeName(root, triple),
		ProjectEntries: container.CountEntries(project),
		Project:        project,
		Binary:         binary,
		Args:           args,
		TTY:            tty.Is(os.Stdin) && tty.Is(os.Stdout),
		UID:            os.Getuid(),
		GID:            os.Getgid(),
		Windows:        runtime.GOOS == "windows",
	}
	if term := os.Getenv("TERM"); term != "" {
		spec.Env = append(spec.Env, "TERM="+term)
	}
	if err := ensureVolume(engine, spec.Image, spec.Platform, spec.Volume, spec.UID, spec.GID); err != nil {
		if down := container.CheckRunning(engine); down != nil {
			return down
		}
		return err
	}
	if installs {
		if info, err := container.GetInfo(engine); err == nil && info.SizeWarning() != "" {
			fmt.Fprintf(s.errOut, "windlock: warning: %s\n", info.SizeWarning())
		}
	}
	cmd := container.Command(spec)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = s.in, s.out, s.errOut
	err = cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return &invoke.ExitError{Code: exit.ExitCode()}
	}
	return err
}

// projectImage is the image the project's lockfile names, else this
// windlock's pin, which init then writes to the lockfile.
func projectImage(project string) string {
	lock, err := lockfile.ReadFile(filepath.Join(project, lockfile.Filename))
	if err == nil && lock.Container.Image != "" {
		return lock.Container.Image
	}
	return tools.ContainerImage
}

// holdRootLock takes the root's lock for a native command that writes it.
// In the container the host launcher already holds it.
func holdRootLock(root string, log io.Writer) (func(), error) {
	if container.Inside() {
		return func() {}, nil
	}
	l, err := rootlock.Acquire(root, log)
	if err != nil {
		return nil, err
	}
	return func() { _ = l.Release() }, nil
}

// newInContainer creates the project on the host, then runs its init and
// target add in the container.
func newInContainer(dir, name, board, revision string, s streams) error {
	initFn := func(appDir, revision string, _ io.Writer) error {
		args := []string{"init"}
		if revision != "" {
			args = append(args, "--zephyr", revision)
		}
		return runInContainer(appDir, args, true, s)
	}
	addFn := func(appDir, name, board, source string, opts setup.TargetOptions, _ io.Writer) error {
		args := []string{"target", "add", name, "--board", board, "--source", source}
		if opts.Default {
			args = append(args, "--default")
		}
		return runInContainer(appDir, args, true, s)
	}
	return setup.NewWith(dir, name, board, revision, s.errOut, initFn, addFn)
}
