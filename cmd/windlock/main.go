// Command windlock installs and manages pinned Zephyr toolchains per project.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"windlock/container"
	"windlock/host"
	"windlock/invoke"
	"windlock/lockfile"
	"windlock/setup"
	"windlock/tools"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// errUsage makes run print the usage text instead of an error line.
var errUsage = errors.New("usage")

type streams struct {
	in          io.Reader
	out, errOut io.Writer
	verbose     bool
	// routing makes inProject stop with errRouted after argument checks.
	routing bool
}

// withLog runs fn with a setup log: progress on stderr, child output in
// <root>/logs (streamed too with -v).
func withLog(s streams, command string, fn func(log io.Writer) error) error {
	root, err := tools.Root()
	if err != nil {
		return err
	}
	release, err := holdRootLock(root, s.errOut)
	if err != nil {
		return err
	}
	defer release()
	o := setup.NewOutput(s.errOut, root, command, s.verbose)
	defer o.Close()
	return fn(o)
}

var commands = map[string]func(args []string, s streams) error{
	// host, read and write are for debugging; not in usage.
	"host":  cmdHost,
	"read":  cmdRead,
	"write": cmdWrite,
	"new":   cmdNew,
	"init":  cmdInit,
	"sync": noArgs(func(s streams) error {
		return s.inProject(func(wd string) error {
			return withLog(s, "sync", func(log io.Writer) error { return setup.Sync(wd, log) })
		})
	}),
	"build":  cmdBuild,
	"west":   cmdWest,
	"shell":  cmdShell,
	"target": cmdTarget,
	"zephyr": cmdZephyr,
	"flash":  cmdProbe("flash"),
	"debug":  cmdProbe("debug"),
	"attach": cmdProbe("attach"),
	// gdb-remote runs in the container for windlock west debug; not in usage.
	"gdb-remote": cmdGDBRemote,
}

func (s streams) child() invoke.Streams {
	return invoke.Streams{In: s.in, Out: s.out, ErrOut: s.errOut}
}

func run(args []string, in io.Reader, out, errOut io.Writer) int {
	verbose := false
	if len(args) > 0 && (args[0] == "-v" || args[0] == "--verbose") {
		verbose, args = true, args[1:]
	}
	if len(args) == 0 {
		usage(errOut)
		return 1
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		usage(out)
		return 0
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(errOut, "windlock: unknown command %q\n", args[0])
		usage(errOut)
		return 1
	}
	err := checkContainerMounts()
	if err == nil {
		err = dispatch(args[0], cmd, args[1:], streams{in: in, out: out, errOut: errOut, verbose: verbose})
	}
	var exit *invoke.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.Code
	case errors.Is(err, errUsage):
		usage(errOut)
	default:
		fmt.Fprintf(errOut, "windlock: %v\n", err)
	}
	return 1
}

func noArgs(fn func(streams) error) func([]string, streams) error {
	return func(args []string, s streams) error {
		if len(args) != 0 {
			return errUsage
		}
		return fn(s)
	}
}

// errRouted stops a command once its arguments are checked, so dispatch can
// run it in the container instead.
var errRouted = errors.New("routed to the container")

// inProject runs fn in the working directory, the application root. When
// routing, it returns errRouted instead.
func (s streams) inProject(fn func(wd string) error) error {
	if s.routing {
		return errRouted
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	return fn(wd)
}

func cmdHost(args []string, s streams) error {
	if len(args) != 0 {
		return errUsage
	}
	triple, err := host.Current()
	if err != nil {
		return err
	}
	fmt.Fprintln(s.out, triple)
	return nil
}

func cmdRead(args []string, s streams) error {
	path, err := optionalPath(args)
	if err != nil {
		return err
	}
	lock, err := lockfile.ReadFile(path)
	if err != nil {
		return err
	}
	return lockfile.Write(s.out, lock)
}

func cmdWrite(args []string, s streams) error {
	path, err := optionalPath(args)
	if err != nil {
		return err
	}
	lock, err := lockfile.Read(s.in)
	if err != nil {
		return err
	}
	return lockfile.WriteFile(path, lock)
}

func cmdWest(args []string, s streams) error {
	if len(args) == 0 {
		return errUsage
	}
	return s.inProject(func(wd string) error { return invoke.West(wd, args, s.child()) })
}

func cmdShell(args []string, s streams) error {
	return s.inProject(func(wd string) error { return invoke.Shell(wd, args, s.child()) })
}

func cmdNew(args []string, s streams) error {
	dir, flagArgs, err := leadingName(args)
	if err != nil {
		return err
	}
	fs := newFlags("new", s)
	name := fs.String("name", "", "application and first target name (default: the directory name)")
	board := fs.String("board", "", "upstream board to install for and add as the default target")
	revision := fs.String("zephyr", "", "first Zephyr tag or branch (default v4.4.2)")
	if err := fs.Parse(flagArgs); err != nil || fs.NArg() != 0 {
		return errUsage
	}
	if !container.Inside() {
		return newInContainer(dir, *name, *board, *revision, s)
	}
	return withLog(s, "new", func(log io.Writer) error { return setup.New(dir, *name, *board, *revision, log) })
}

func cmdInit(args []string, s streams) error {
	fs := newFlags("init", s)
	revision := fs.String("zephyr", "", "first Zephyr tag or branch of a new windlock.yml")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errUsage
	}
	return s.inProject(func(wd string) error {
		return withLog(s, "init", func(log io.Writer) error { return setup.Init(wd, *revision, log) })
	})
}

// cmdBuild takes [name] [west build options] [-- cmake args]: the first
// argument is the target unless it is an option or --.
func cmdBuild(args []string, s streams) error {
	name, extra := "", args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		if args[0] == "" {
			return errUsage
		}
		name, extra = args[0], args[1:]
	}
	return s.inProject(func(wd string) error { return invoke.Build(wd, name, extra, s.child()) })
}

func cmdTarget(args []string, s streams) error {
	if len(args) == 0 {
		return errUsage
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		name, flagArgs, err := leadingName(rest)
		if err != nil {
			return err
		}
		fs := newFlags("target add", s)
		board := fs.String("board", "", "Zephyr board name")
		source := fs.String("source", "", "application directory, relative to the project")
		makeDefault := fs.Bool("default", false, "make it the default target")
		sysbuild := fs.Bool("sysbuild", false, "build with sysbuild (default: detected from the source)")
		noSysbuild := fs.Bool("no-sysbuild", false, "build without sysbuild even if the source has sysbuild files")
		if err := fs.Parse(flagArgs); err != nil || fs.NArg() != 0 || *board == "" || *source == "" || (*sysbuild && *noSysbuild) {
			return errUsage
		}
		opts := setup.TargetOptions{Default: *makeDefault}
		if *sysbuild || *noSysbuild {
			on := *sysbuild
			opts.Sysbuild = &on
		}
		return s.inProject(func(wd string) error {
			return withLog(s, "target-add", func(log io.Writer) error { return setup.TargetAdd(wd, name, *board, *source, opts, log) })
		})
	case "remove", "default":
		name, err := oneName(rest)
		if err != nil {
			return err
		}
		if sub == "remove" {
			return s.inProject(func(wd string) error {
				return withLog(s, "target-remove", func(log io.Writer) error { return setup.TargetRemove(wd, name, log) })
			})
		}
		return s.inProject(func(wd string) error { return setup.TargetDefault(wd, name, s.errOut) })
	case "list":
		if len(rest) != 0 {
			return errUsage
		}
		return s.inProject(func(wd string) error { return setup.TargetList(wd, s.out, s.errOut) })
	default:
		return fmt.Errorf("unknown target command %q; use add, remove, default, or list", sub)
	}
}

func cmdZephyr(args []string, s streams) error {
	if len(args) == 0 {
		return errUsage
	}
	sub, rest := args[0], args[1:]
	if sub == "list" {
		if len(rest) != 0 {
			return errUsage
		}
		return s.inProject(func(wd string) error { return setup.ZephyrList(wd, s.out, s.errOut) })
	}
	fn := map[string]func(appDir, revision string, log io.Writer) error{
		"add":     setup.ZephyrAdd,
		"remove":  setup.ZephyrRemove,
		"default": setup.ZephyrDefault,
	}[sub]
	if fn == nil {
		return fmt.Errorf("unknown zephyr command %q; use add, remove, default, or list", sub)
	}
	revision, err := oneName(rest)
	if err != nil {
		return err
	}
	return s.inProject(func(wd string) error {
		return withLog(s, "zephyr-"+sub, func(log io.Writer) error { return fn(wd, revision, log) })
	})
}

func newFlags(name string, s streams) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(s.errOut)
	fs.Usage = func() {}
	return fs
}

// leadingName splits "<name> [flags]": the flag package stops at the first
// non-flag argument, so the name must come first.
func leadingName(args []string) (string, []string, error) {
	if len(args) == 0 || args[0] == "" || strings.HasPrefix(args[0], "-") {
		return "", nil, errUsage
	}
	return args[0], args[1:], nil
}

func oneName(args []string) (string, error) {
	if len(args) != 1 || args[0] == "" || strings.HasPrefix(args[0], "-") {
		return "", errUsage
	}
	return args[0], nil
}

func optionalPath(args []string) (string, error) {
	switch len(args) {
	case 0:
		return lockfile.Filename, nil
	case 1:
		return args[0], nil
	default:
		return "", errUsage
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `usage: windlock [-v] <command> [arguments]

Reproducible Zephyr builds from one lockfile. Run commands in the project root.

Project:
  new <dir> [--name <app>] [--board <board>] [--zephyr <rev>]
                 create a project; --board also installs it and adds a target
  init [--zephyr <rev>]
                 install what %[1]s names and write the lockfile
  sync           install what the lockfile names and check it matches

Build and run:
  build [name] [west options] [-- cmake args]
                 build a target in the container
  flash [name] [--no-build] [-r <runner>] [-- west options]
                 build, then flash from this machine
  debug [name] [--no-build] [-r <runner>] [-- west options]
                 build, then debug from this machine
  attach [name] [-r <runner>] [-- west options]
                 attach gdb to the running target
  west <args>    run any other west command
  shell [args]   open a shell in the build environment

Manage:
  target add <name> --board <board> --source <dir> [--default]
                 [--sysbuild|--no-sysbuild]
  target remove|default <name>
  target list
  zephyr add|remove|default <rev>
  zephyr list

Options:
  -v, --verbose  show tool output as it runs (default: logs/ in the root)
  -h, --help     show this help

Environment:
  %-15[3]s toolchain root (default ~/.local/share/windlock)
  %-15[4]s container CLI: docker, podman or nerdctl (default: first found)

Commit %[1]s and %[2]s; don't edit them by hand.
`, lockfile.ConfigFilename, lockfile.Filename, tools.RootEnv, container.EngineEnv)
}
