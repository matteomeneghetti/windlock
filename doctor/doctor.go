// Package doctor checks whether this machine and project are ready, and says
// how to fix what is not. It writes nothing on the host; the deeper checks
// need a throwaway container, and are skipped until the image is there.
package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"windlock/container"
	"windlock/flash"
	"windlock/lockfile"
	"windlock/setup"
)

// Status is how a check went.
type Status string

const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
	Skip Status = "skip"
)

// minFreeBytes is roughly what the image and a first toolchain install take.
const minFreeBytes = 20 << 30

// Result is one check's outcome.
type Result struct {
	Status Status
	Name   string
	Detail string
	Fix    string
}

// Failed reports whether any result is a failure.
func Failed(results []Result) bool {
	for _, r := range results {
		if r.Status == Fail {
			return true
		}
	}
	return false
}

// Config is what the checks look at; tests stand in for the lookups.
type Config struct {
	Project string // the working directory
	Root    string // the toolchain root
	GOOS    string
	GOARCH  string
	// LookPath finds a program on PATH.
	LookPath func(string) (string, error)
	// FreeBytes is the space free on the disk holding a path.
	FreeBytes func(string) (uint64, error)
}

// Default is the Config for this machine.
func Default(project, root string) Config {
	return Config{
		Project: project, Root: root, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		LookPath: exec.LookPath, FreeBytes: freeBytes,
	}
}

// Run runs every check, in order.
func Run(cfg Config) []Result {
	var results []Result
	add := func(status Status, name, detail, fix string) {
		results = append(results, Result{status, name, detail, fix})
	}
	engine, engineErr := container.FindEngine()
	running := false
	if engineErr != nil {
		add(Fail, "engine", "no container engine found", "install one, e.g. brew install colima docker; or set "+container.EngineEnv)
		add(Skip, "running", "no engine", "")
	} else {
		add(OK, "engine", filepath.Base(engine.Path), "")
		if err := container.CheckRunning(engine); err != nil {
			add(Fail, "running", filepath.Base(engine.Path)+" isn't responding", "start it (Colima: colima start)")
		} else {
			running = true
			add(OK, "running", "", "")
		}
	}
	results = append(results, vmSize(engine, running))
	triple, tripleErr := container.Triple(cfg.GOARCH)
	volume := ""
	if tripleErr == nil {
		volume = container.VolumeName(cfg.Root, triple)
	}
	haveVolume := running && volume != "" && container.VolumeExists(engine, volume)
	results = append(results, diskSpace(cfg, haveVolume))
	results = append(results, compiler(cfg))
	return append(results, projectChecks(cfg, engine, running, triple, volume, haveVolume)...)
}

func vmSize(engine container.Engine, running bool) Result {
	if !running {
		return Result{Skip, "vm size", "engine not running", ""}
	}
	info, err := container.GetInfo(engine)
	if err != nil {
		return Result{Skip, "vm size", err.Error(), ""}
	}
	detail := fmt.Sprintf("%d CPUs, %.1f GiB", info.CPUs, float64(info.Memory)/(1<<30))
	if info.SizeWarning() != "" {
		return Result{Warn, "vm size", detail, "4 CPUs and 8 GB are recommended (Colima: colima stop && colima start --cpu 4 --memory 8)"}
	}
	return Result{OK, "vm size", detail, ""}
}

// diskSpace matters only before the first install, when there is no volume.
func diskSpace(cfg Config, haveVolume bool) Result {
	if haveVolume {
		return Result{Skip, "disk", "toolchain already installed", ""}
	}
	path := cfg.Root
	for {
		if _, err := os.Stat(path); err == nil || filepath.Dir(path) == path {
			break
		}
		path = filepath.Dir(path)
	}
	free, err := cfg.FreeBytes(path)
	if err != nil {
		return Result{Skip, "disk", err.Error(), ""}
	}
	detail := fmt.Sprintf("%d GB free", free>>30)
	if free < minFreeBytes {
		return Result{Warn, "disk", detail, "a first install needs about 20 GB"}
	}
	return Result{OK, "disk", detail, ""}
}

// compiler checks what building dfu-util needs. On macOS /usr/bin/cc exists
// without the command-line tools, so xcode-select is what says they are there.
func compiler(cfg Config) Result {
	fix := "flashing builds dfu-util, which needs a C compiler and make"
	if cfg.GOOS == "darwin" {
		fix = "flashing builds dfu-util; install the tools with xcode-select --install"
		if exec.Command("xcode-select", "-p").Run() != nil {
			return Result{Warn, "compiler", "Xcode command-line tools not found", fix}
		}
	}
	for _, name := range []string{"cc", "make"} {
		if _, err := cfg.LookPath(name); err != nil {
			return Result{Warn, "compiler", name + " not found", fix}
		}
	}
	return Result{OK, "compiler", "cc, make", ""}
}

func projectChecks(cfg Config, engine container.Engine, running bool, triple, volume string, haveVolume bool) []Result {
	configPath := filepath.Join(cfg.Project, lockfile.ConfigFilename)
	lockPath := filepath.Join(cfg.Project, lockfile.Filename)
	if !exists(configPath) && !exists(lockPath) {
		return []Result{{Skip, "project", "not in a windlock project", ""}}
	}
	conf, err := lockfile.ReadConfigFile(configPath)
	if err != nil {
		return []Result{{Fail, "config", err.Error(), "run windlock init"}}
	}
	lock, err := lockfile.ReadFile(lockPath)
	if err != nil {
		return []Result{{Fail, "lockfile", err.Error(), "run windlock init"}}
	}
	if lock.Container.Image == "" {
		return []Result{{Fail, "lockfile", "it pins no container image", "run windlock init"}}
	}
	entry, err := lock.DefaultEntry()
	if err != nil {
		return []Result{{Fail, "lockfile", err.Error(), "run windlock init"}}
	}
	results := []Result{{OK, "lockfile", lockfile.Filename, ""}}
	results = append(results, install(cfg, engine, running, triple, volume, haveVolume, lock, entry)...)
	return append(results, vendorTool(cfg, conf))
}

// install checks the image, the volume, and through a throwaway container the
// volume's contents and that the engine can see the project.
func install(cfg Config, engine container.Engine, running bool, triple, volume string, haveVolume bool, lock lockfile.Lockfile, entry lockfile.Zephyr) []Result {
	if !running {
		return []Result{{Skip, "install", "engine not running", ""}}
	}
	if !container.ImageExists(engine, lock.Container.Image) {
		return []Result{
			{Warn, "image", "not pulled yet", "windlock sync pulls it"},
			{Skip, "install", "needs the image", ""},
		}
	}
	results := []Result{{OK, "image", "pulled", ""}}
	if !haveVolume {
		return append(results, Result{Warn, "install", "toolchain not installed", "run windlock sync"})
	}
	workspace, err := setup.WorkspaceDir(container.RootPath, cfg.Project, entry.Commit)
	if err != nil {
		return append(results, Result{Fail, "install", err.Error(), "run windlock init"})
	}
	sdk := filepath.Join(container.RootPath, "sdk", entry.SDK, triple, "zephyr-sdk-"+entry.SDK)
	script := fmt.Sprintf(`test -d '%s/zephyr' && echo workspace; test -d '%s' && echo sdk; echo entries $(ls -A %s | wc -l)`,
		workspace, sdk, container.ProjectMount)
	out, err := container.Inspect(engine, lock.Container.Image, "linux/"+cfg.GOARCH, volume, cfg.Project, script)
	if err != nil {
		return append(results, Result{Skip, "install", err.Error(), ""})
	}
	if strings.Contains(out, "workspace") && strings.Contains(out, "sdk") {
		results = append(results, Result{OK, "install", "SDK " + entry.SDK + ", Zephyr " + entry.Name, ""})
	} else {
		results = append(results, Result{Warn, "install", "the SDK or Zephyr workspace is missing", "run windlock sync"})
	}
	if seesProject(out) {
		results = append(results, Result{OK, "project", "visible to the engine", ""})
	} else {
		results = append(results, Result{Fail, "project", "empty inside the container", container.NotSharedFix(cfg.Project)})
	}
	return results
}

func seesProject(inspectOutput string) bool {
	for _, line := range strings.Split(inspectOutput, "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "entries" {
			n, err := strconv.Atoi(fields[1])
			return err == nil && n > 0
		}
	}
	return false
}

// vendorTool reports on the default flash runner, once a build names one.
func vendorTool(cfg Config, conf lockfile.Config) Result {
	target, err := conf.TargetNamed("")
	if err != nil {
		return Result{Skip, "flash", "no default target", ""}
	}
	runner, err := flash.VendorProblem(filepath.Join(cfg.Project, "build", target.Name), flash.Tools{LookPath: cfg.LookPath})
	if err != nil {
		return Result{Warn, "flash", "default runner " + runner + " needs a tool you install yourself", err.Error()}
	}
	if runner == "" {
		return Result{Skip, "flash", "not built yet", ""}
	}
	return Result{OK, "flash", "default runner " + runner, ""}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
