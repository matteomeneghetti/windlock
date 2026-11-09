package tools

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/matteomeneghetti/windlock/internal/envvar"
)

func installArchive(root string, a Archive, log io.Writer) (string, error) {
	fmt.Fprintf(log, "%s: checking cache\n", a.Name)
	path, cached, err := ensureArchive(root, a.URL, a.SHA256, a.File)
	if err != nil {
		return "", err
	}
	if cached {
		fmt.Fprintf(log, "%s: cached archive matches sha256\n", a.Name)
	} else {
		fmt.Fprintf(log, "%s: downloaded %s\n", a.Name, a.File)
	}
	return path, nil
}

func stageExtract(root, sum, archive string, strip int) (string, error) {
	stage := filepath.Join(root, "cache", "extract", sum)
	if err := os.RemoveAll(stage); err != nil {
		return "", err
	}
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return "", err
	}
	if err := extractTar(archive, stage, strip); err != nil {
		_ = os.RemoveAll(stage)
		return "", err
	}
	return stage, nil
}

func buildAutotools(root, archive, srcDir, prefix string, extraPATH []string, log io.Writer) error {
	return buildAutotoolsWith(root, archive, srcDir, prefix, commandEnv(extraPATH), nil, log)
}

// buildAutotoolsWith runs configure --prefix=prefix with args, make and make
// install for a source archive, in env.
func buildAutotoolsWith(root, archive, srcDir, prefix string, env, args []string, log io.Writer) error {
	srcParent := filepath.Join(root, "cache", "build")
	src := filepath.Join(srcParent, srcDir)
	if err := os.RemoveAll(src); err != nil {
		return err
	}
	if err := os.MkdirAll(srcParent, 0o755); err != nil {
		return err
	}
	if err := extractTar(archive, srcParent, 0); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(src, "configure")); err != nil {
		return fmt.Errorf("%s has no configure", srcDir)
	}
	fmt.Fprintf(log, "%s: configuring\n", srcDir)
	if err := runLogged(log, src, env, filepath.Join(src, "configure"), append([]string{"--prefix=" + prefix}, args...)...); err != nil {
		return err
	}
	jobs := strconv.Itoa(jobCount())
	fmt.Fprintf(log, "%s: building\n", srcDir)
	if err := runLogged(log, src, env, "make", "-j"+jobs); err != nil {
		return err
	}
	fmt.Fprintf(log, "%s: installing\n", srcDir)
	return runLogged(log, src, env, "make", "install")
}

func ensureBuildTools(root string, log io.Writer) error {
	prefix := filepath.Join(root, "cache", "build-tools")
	bin := filepath.Join(prefix, "bin")
	marker := func(name string) string { return filepath.Join(prefix, ".installed", name) }
	done := map[string]bool{}
	for _, dep := range dtcBuildDeps {
		done[dep.Name] = installed(marker(dep.Name), filepath.Join(bin, dep.Name), dep.SHA256)
	}
	if done["m4"] && done["flex"] && done["bison"] {
		return nil
	}
	var path []string
	if done["m4"] {
		path = []string{bin}
	}
	for _, dep := range dtcBuildDeps {
		if done[dep.Name] {
			continue
		}
		arc, err := installArchive(root, dep, log)
		if err != nil {
			return err
		}
		if err := buildAutotools(root, arc, dep.SrcDir, prefix, path, log); err != nil {
			return fmt.Errorf("%s: %w", dep.Name, err)
		}
		if _, err := os.Stat(filepath.Join(bin, dep.Name)); err != nil {
			return fmt.Errorf("build tool %s was not installed", dep.Name)
		}
		if err := copyToMode(strings.NewReader(dep.SHA256+"\n"), marker(dep.Name), 0o644); err != nil {
			return err
		}
		path = []string{bin}
	}
	return nil
}

func buildDTC(root, archive, srcDir, bin string, log io.Writer) error {
	if err := ensureBuildTools(root, log); err != nil {
		return err
	}
	toolsBin := filepath.Join(root, "cache", "build-tools", "bin")
	srcParent := filepath.Join(root, "cache", "build")
	src := filepath.Join(srcParent, srcDir)
	if err := os.RemoveAll(src); err != nil {
		return err
	}
	if err := os.MkdirAll(srcParent, 0o755); err != nil {
		return err
	}
	if err := extractTar(archive, srcParent, 0); err != nil {
		return err
	}
	env := commandEnv([]string{toolsBin})
	fmt.Fprintf(log, "dtc: building\n")
	// NO_YAML=1 drops libyaml, but the Makefile still runs $(PKG_CONFIG), which
	// fails on hosts without pkg-config; pointing it at false avoids shipping one.
	err := runLogged(log, src, env, "make", dtcMakeArgs(jobCount(), toolsBin)...)
	if err != nil {
		return err
	}
	return copyFile(filepath.Join(src, "dtc"), filepath.Join(bin, "dtc"), 0o755)
}

func dtcMakeArgs(jobs int, toolsBin string) []string {
	return []string{
		"-j" + strconv.Itoa(jobs),
		"dtc",
		"LEX=" + filepath.Join(toolsBin, "flex"),
		"BISON=" + filepath.Join(toolsBin, "bison"),
		"NO_YAML=1",
		"PKG_CONFIG=/usr/bin/false",
	}
}

func commandEnv(extraPATH []string) []string {
	env := os.Environ()
	if len(extraPATH) == 0 {
		return env
	}
	merged := strings.Join(extraPATH, string(os.PathListSeparator))
	if cur := os.Getenv("PATH"); cur != "" {
		merged += string(os.PathListSeparator) + cur
	}
	return envvar.Set(env, "PATH", merged)
}

func runLogged(log io.Writer, dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		text := buf.String()
		if len(text) > 8000 {
			text = text[len(text)-8000:]
		}
		fmt.Fprintf(log, "%s\n", text)
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func jobCount() int {
	n := runtime.NumCPU()
	if n < 1 {
		return 1
	}
	return n
}
