// Package container runs windlock commands inside the pinned Linux image,
// with the project and the container's toolchain root mounted from the host.
package container

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// EngineEnv names the container CLI to use instead of the first found.
	EngineEnv = "WINDLOCK_ENGINE"
	// insideEnv is set in the container, where windlock runs natively.
	insideEnv = "WINDLOCK_IN_CONTAINER"
	// entriesEnv is how many entries the host sees in the project directory.
	entriesEnv = "WINDLOCK_PROJECT_ENTRIES"
	// RootPath is where the volume holding the container's toolchain root
	// is mounted.
	RootPath = "/windlock/root"
	// binaryPath is where the Linux windlock is mounted in the container.
	binaryPath = "/usr/local/bin/windlock"
	// minCPUs and minMemory are the VM size below which windlock warns. The
	// memory is under 8 GiB because a VM set to 8 GB reports a little less.
	minCPUs   = 4
	minMemory = 7 << 30
)

// Inside reports whether this process runs in the container.
func Inside() bool { return os.Getenv(insideEnv) == "1" }

// Engine is a Docker-compatible CLI: docker, podman or nerdctl.
type Engine struct {
	Path   string
	Podman bool
}

// FindEngine returns WINDLOCK_ENGINE, else the first of docker, podman and
// nerdctl on PATH.
func FindEngine() (Engine, error) {
	names := []string{"docker", "podman", "nerdctl"}
	if v := os.Getenv(EngineEnv); v != "" {
		names = []string{v}
	}
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			return Engine{Path: p, Podman: strings.HasPrefix(filepath.Base(p), "podman")}, nil
		}
	}
	if v := os.Getenv(EngineEnv); v != "" {
		return Engine{}, fmt.Errorf("%s=%s is not on PATH", EngineEnv, v)
	}
	return Engine{}, fmt.Errorf("windlock builds in a container and needs docker, podman or nerdctl on PATH (e.g. colima, OrbStack or Docker Desktop)")
}

// Info is what the engine reports about the machine containers run on.
type Info struct {
	CPUs   int
	Memory int64 // bytes
}

// engineInfo runs the engine's info command, which fails when the engine's
// daemon or VM is down. The timeout covers a VM that hangs instead of failing.
func engineInfo(e Engine) (string, error) {
	format := "{{.NCPU}} {{.MemTotal}}"
	if e.Podman {
		format = "{{.Host.CPUs}} {{.Host.MemTotal}}"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, e.Path, "info", "--format", format).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && len(exit.Stderr) > 0 {
		return "", errors.New(strings.TrimSpace(string(exit.Stderr)))
	}
	return string(out), err
}

// GetInfo returns the engine's CPUs and memory.
func GetInfo(e Engine) (Info, error) {
	text, err := engineInfo(e)
	if err != nil {
		return Info{}, err
	}
	var info Info
	if _, err := fmt.Sscan(text, &info.CPUs, &info.Memory); err != nil {
		return Info{}, fmt.Errorf("unexpected %s info output %q", filepath.Base(e.Path), strings.TrimSpace(text))
	}
	return info, nil
}

// CheckRunning returns an error saying how to start the engine if it is down.
func CheckRunning(e Engine) error {
	if _, err := engineInfo(e); err != nil {
		return fmt.Errorf("%s isn't responding, so the container engine may not be running. Start it (Colima: colima start) and try again: %v", filepath.Base(e.Path), err)
	}
	return nil
}

// SizeWarning says how to enlarge a VM too small for a toolchain install and
// firmware builds; it is empty for one that is large enough.
func (i Info) SizeWarning() string {
	if i.CPUs >= minCPUs && i.Memory >= minMemory {
		return ""
	}
	return fmt.Sprintf("the container engine has %d CPUs and %.1f GiB of memory, and installs and builds may fail or be slow; 4 CPUs and 8 GB are recommended (Colima: colima stop && colima start --cpu 4 --memory 8)",
		i.CPUs, float64(i.Memory)/(1<<30))
}

// ImageExists reports whether the engine already holds image.
func ImageExists(e Engine, image string) bool {
	return exec.Command(e.Path, "image", "inspect", image).Run() == nil
}

// VolumeExists reports whether the engine already holds volume.
func VolumeExists(e Engine, volume string) bool {
	return exec.Command(e.Path, "volume", "inspect", volume).Run() == nil
}

// ProjectMount is where Inspect mounts the project.
const ProjectMount = "/windlock-project"

// Inspect runs a shell script in a throwaway container, with the volume and
// the project mounted read-only, and returns what it prints.
func Inspect(e Engine, image, platform, volume, project, script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	args := []string{"run", "--rm"}
	if platform != "" {
		args = append(args, "--platform", platform)
	}
	args = append(args, "-v", volume+":"+RootPath+":ro", "-v", project+":"+ProjectMount+":ro",
		"--entrypoint", "/bin/sh", image, "-c", script)
	out, err := exec.CommandContext(ctx, e.Path, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// NotSharedFix says how to share dir with the engine's VM.
func NotSharedFix(dir string) string { return notShared(dir).Error() }

// Triple is the Linux host triple the container runs as. It matches the
// host's CPU, because an emulated container is several times slower.
func Triple(goarch string) (string, error) {
	switch goarch {
	case "arm64":
		return "linux-aarch64", nil
	case "amd64":
		return "linux-x86_64", nil
	}
	return "", fmt.Errorf("no container platform for GOARCH=%s", goarch)
}

// VolumeName is the engine volume holding the container's toolchain root
// for root and triple. A volume, not a shared folder: file sharing from macOS
// (virtiofs) cannot open a file it just created with mode 000, which GNU tar
// relies on, so the SDK's installer fails there.
func VolumeName(root, triple string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return "windlock-" + triple + "-" + hex.EncodeToString(sum[:4])
}

// EnsureVolume creates the volume on first use, owned by uid:gid, with the
// home directory the container uses.
func EnsureVolume(e Engine, image, platform, volume string, uid, gid int) error {
	if err := exec.Command(e.Path, "volume", "inspect", volume).Run(); err == nil {
		return nil
	}
	if out, err := exec.Command(e.Path, "volume", "create", volume).CombinedOutput(); err != nil {
		return fmt.Errorf("%s volume create %s: %v: %s", filepath.Base(e.Path), volume, err, strings.TrimSpace(string(out)))
	}
	script := "mkdir -p " + RootPath + "/home"
	if uid >= 0 && gid >= 0 {
		script += fmt.Sprintf(" && chown -R %d:%d %s", uid, gid, RootPath)
	}
	args := []string{"run", "--rm", "--user", "0:0"}
	if platform != "" {
		args = append(args, "--platform", platform)
	}
	args = append(args, "-v", volume+":"+RootPath, "--entrypoint", "/bin/sh", image, "-c", script)
	if out, err := exec.Command(e.Path, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("preparing volume %s: %v: %s", volume, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Spec is one windlock command to run in the container.
type Spec struct {
	Engine   Engine
	Image    string
	Platform string   // linux/arm64 or linux/amd64
	Volume   string   // engine volume holding the container's toolchain root
	Project  string   // host project directory; the working directory inside
	Binary   string   // the Linux windlock on the host
	Args     []string // windlock arguments
	TTY      bool
	// UID and GID run the container as the host user, so files it writes
	// stay the user's; -1 keeps the image's user.
	UID, GID int
	// Windows maps the project to /work/<name>, since its path cannot match.
	Windows bool
	Env     []string // KEY=VALUE pairs passed through
	// ProjectEntries is how many entries the host sees in Project; the
	// container checks it sees them too.
	ProjectEntries int
}

// Argv is the engine's command line for s, without the engine itself.
func (s Spec) Argv() []string {
	project := s.Project
	if s.Windows && project != "" {
		project = "/work/" + windowsBase(project)
	}
	args := []string{"run", "--rm", "-i"}
	if s.TTY {
		args = append(args, "-t")
	}
	if s.Engine.Podman {
		args = append(args, "--userns=keep-id")
	}
	if s.UID >= 0 && s.GID >= 0 {
		args = append(args, "--user", fmt.Sprintf("%d:%d", s.UID, s.GID))
	}
	if s.Platform != "" {
		args = append(args, "--platform", s.Platform)
	}
	args = append(args, "-v", s.Volume+":"+RootPath)
	if project != "" {
		args = append(args, "-v", s.Project+":"+project, "-w", project)
	}
	args = append(args, "-v", s.Binary+":"+binaryPath+":ro")
	args = append(args,
		"-e", insideEnv+"=1",
		"-e", "WINDLOCK_ROOT="+RootPath,
		"-e", "HOME="+RootPath+"/home",
		"-e", fmt.Sprintf("%s=%d", entriesEnv, s.ProjectEntries),
	)
	for _, kv := range s.Env {
		args = append(args, "-e", kv)
	}
	args = append(args, "--entrypoint", binaryPath, s.Image)
	return append(args, s.Args...)
}

func windowsBase(p string) string {
	p = strings.TrimRight(p, `\/`)
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// CountEntries is how many entries dir holds, for Spec.ProjectEntries.
func CountEntries(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	return len(entries)
}

// CheckMounts runs in the container. An engine whose VM does not share a
// host folder mounts an empty one instead, so windlock would build nothing
// and write where the host cannot see; it fails instead, naming the folder.
func CheckMounts(project string) error {
	host, err := strconv.Atoi(os.Getenv(entriesEnv))
	if err != nil || host == 0 {
		return nil
	}
	if CountEntries(project) == 0 {
		return notShared(project)
	}
	return nil
}

func notShared(dir string) error {
	return fmt.Errorf("%s is empty inside the container: the container engine's VM does not share it with the host. "+
		"Share it (colima: add it under mounts: in ~/.colima/default/colima.yaml, then colima restart; Docker Desktop: Settings > Resources > File sharing), "+
		"or move the project under a shared folder such as your home directory", dir)
}

// CopyOut copies names under dir in the volume into dst on the host. tar
// streams them through stdout, so no file sharing is involved.
func CopyOut(e Engine, image, platform, volume, dir, dst string, names ...string) error {
	args := []string{"run", "--rm"}
	if platform != "" {
		args = append(args, "--platform", platform)
	}
	args = append(args, "-v", volume+":"+RootPath+":ro", "--entrypoint", "/bin/tar", image, "-C", dir, "-c")
	args = append(args, names...)
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	out := exec.Command(e.Path, args...)
	in := exec.Command("tar", "-x", "-C", dst)
	out.Stdout, in.Stdin = w, r
	var outErr, inErr strings.Builder
	out.Stderr, in.Stderr = &outErr, &inErr
	startErr := in.Start()
	if startErr == nil {
		startErr = out.Start()
	}
	// The children hold their own ends; closing ours lets tar see EOF.
	w.Close()
	r.Close()
	if startErr != nil {
		if in.Process != nil {
			_ = in.Wait()
		}
		return startErr
	}
	outWait, inWait := out.Wait(), in.Wait()
	if outWait != nil {
		return fmt.Errorf("%s run tar: %v: %s", filepath.Base(e.Path), outWait, strings.TrimSpace(outErr.String()))
	}
	if inWait != nil {
		return fmt.Errorf("tar -x: %v: %s", inWait, strings.TrimSpace(inErr.String()))
	}
	return nil
}

// Command is the engine process for s. The caller sets its streams.
func Command(s Spec) *exec.Cmd {
	return exec.Command(s.Engine.Path, s.Argv()...)
}
