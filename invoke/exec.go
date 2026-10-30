// Package invoke runs west and shells in a project's isolated environment.
package invoke

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"windlock/host"
	"windlock/internal/envvar"
	"windlock/lockfile"
	"windlock/setup"
	"windlock/tools"
)

// ExitError is a child that ran and exited non-zero. The child has already
// printed its error, so the CLI exits with Code and adds nothing.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// Streams are the standard streams handed to a child.
type Streams struct {
	In          io.Reader
	Out, ErrOut io.Writer
}

// Build runs west build for the named windlock.yml target, or the default
// one, with extra west build options and CMake arguments.
func Build(appDir, targetName string, extra []string, s Streams) error {
	if err := setup.InstallBoardToolchain(appDir, targetName, s.ErrOut); err != nil {
		return err
	}
	cfg, err := lockfile.ReadConfigFile(filepath.Join(appDir, lockfile.ConfigFilename))
	if err != nil {
		return err
	}
	lock, root, triple, err := project(appDir)
	if err != nil {
		return err
	}
	cmd, err := buildCommand(cfg, lock, root, triple, appDir, os.Getenv("PATH"), targetName, extra)
	if err != nil {
		return err
	}
	target, err := cfg.TargetNamed(targetName)
	if err != nil {
		return err
	}
	buildDir := filepath.Join(appDir, "build", target.Name)
	if err := removeStaleBuild(buildDir, s.ErrOut); err != nil {
		return err
	}
	fmt.Fprintf(s.ErrOut, "ZEPHYR_BASE=%s\n", envvar.Get(cmd.Env, "ZEPHYR_BASE"))
	fmt.Fprintf(s.ErrOut, "ZEPHYR_SDK_INSTALL_DIR=%s\n", envvar.Get(cmd.Env, "ZEPHYR_SDK_INSTALL_DIR"))
	fmt.Fprintf(s.ErrOut, "west %s\n", strings.Join(cmd.Args, " "))
	err = start(cmd, cmd.West, cmd.Args, s)
	// Even a failed build has usually configured the directory.
	if recErr := recordContainer(buildDir); err == nil {
		err = recErr
	}
	return err
}

// guarded are west commands that write outside the toolchain root or move
// the workspace away from the lockfile, with the windlock way instead.
var guarded = map[string]string{
	"zephyr-export": "it registers Zephyr in ~/.cmake/packages; windlock sets ZEPHYR_BASE, so it is not needed",
	"sdk":           "it installs SDKs outside the toolchain root; windlock sync installs the SDK the lockfile names",
	"init":          "it would replace the workspace windlock manages; use windlock zephyr add",
	"update":        "it would move the workspace away from " + lockfile.Filename + "; use windlock sync",
}

// West runs any west command in the project's isolated environment, with
// the caller's standard streams, so menuconfig and debug stay interactive.
func West(appDir string, args []string, s Streams) error {
	if sub := WestSubcommand(args); guarded[sub] != "" {
		return fmt.Errorf("west %s is refused: %s", sub, guarded[sub])
	}
	cmd, err := environment(appDir)
	if err != nil {
		return err
	}
	return start(cmd, cmd.West, args, s)
}

// WestSubcommand is the first argument after west's own options.
func WestSubcommand(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// Shell starts $SHELL, or /bin/sh, in the project's isolated environment.
// It cannot guard what is typed, so it states the rule once.
func Shell(appDir string, args []string, s Streams) error {
	cmd, err := environment(appDir)
	if err != nil {
		return err
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd.Env = envvar.Set(cmd.Env, "WINDLOCK_PROJECT", appDir)
	fmt.Fprintf(s.ErrOut, "windlock shell for %s; exit to leave. Avoid west zephyr-export, sdk, init, and update here: they write outside the toolchain root or move the workspace off %s.\n",
		appDir, lockfile.Filename)
	return start(cmd, shell, args, s)
}

// start runs name with args, mapping a non-zero exit to ExitError.
func start(cmd Command, name string, args []string, s Streams) error {
	proc := exec.Command(name, args...)
	proc.Dir = cmd.Dir
	proc.Env = cmd.Env
	proc.Stdin = s.In
	proc.Stdout = s.Out
	proc.Stderr = s.ErrOut
	err := proc.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return &ExitError{Code: exit.ExitCode()}
	}
	return err
}

func environment(appDir string) (Command, error) {
	lock, root, triple, err := project(appDir)
	if err != nil {
		return Command{}, err
	}
	return workspaceEnv(lock, root, triple, appDir, os.Getenv("PATH"))
}

func project(appDir string) (lockfile.Lockfile, string, string, error) {
	lock, err := lockfile.ReadFile(filepath.Join(appDir, lockfile.Filename))
	if err != nil {
		if os.IsNotExist(err) {
			return lockfile.Lockfile{}, "", "", fmt.Errorf("%w; run windlock init first", err)
		}
		return lockfile.Lockfile{}, "", "", err
	}
	root, err := tools.Root()
	if err != nil {
		return lockfile.Lockfile{}, "", "", err
	}
	triple, err := host.Current()
	if err != nil {
		return lockfile.Lockfile{}, "", "", err
	}
	return lock, root, triple, nil
}
