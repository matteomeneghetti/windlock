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
	"sync"
)

func installArchive(root string, t Tool, log io.Writer) (string, error) {
	fmt.Fprintf(log, "%s: checking cache\n", t.Name)
	file := fileName(t)
	path, cached, err := ensureArchive(root, t.URL, t.SHA256, file)
	if err != nil {
		return "", err
	}
	if cached {
		fmt.Fprintf(log, "%s: cached archive matches sha256\n", t.Name)
	} else {
		fmt.Fprintf(log, "%s: downloaded %s\n", t.Name, file)
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

// extractSource unpacks a source tarball into a fresh cache/build/<sum> and
// returns its single top directory, whatever the version names it.
func extractSource(root, archive, sum string) (string, error) {
	dir := filepath.Join(root, "cache", "build", strings.ToLower(sum))
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := extractTar(archive, dir, 0); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		return "", fmt.Errorf("%s does not unpack to one top directory", filepath.Base(archive))
	}
	return filepath.Join(dir, entries[0].Name()), nil
}

// buildFromSource runs configure --prefix=prefix with args, make and make
// install for a source archive, in the clean build environment with
// extraPATH first on its PATH.
func buildFromSource(root, archive, sum, prefix string, extraPATH, args []string, log io.Writer) error {
	return buildFromSourceEnv(root, archive, sum, prefix, buildEnv(root, extraPATH), args, log)
}

func buildFromSourceEnv(root, archive, sum, prefix string, env, args []string, log io.Writer) error {
	src, err := extractSource(root, archive, sum)
	if err != nil {
		return err
	}
	name := filepath.Base(src)
	if _, err := os.Stat(filepath.Join(src, "configure")); err != nil {
		return fmt.Errorf("%s has no configure", name)
	}
	fmt.Fprintf(log, "%s: configuring\n", name)
	if err := runLogged(log, src, env, filepath.Join(src, "configure"), append([]string{"--prefix=" + prefix}, args...)...); err != nil {
		return err
	}
	fmt.Fprintf(log, "%s: building\n", name)
	if err := runLogged(log, src, env, "make", "-j"+strconv.Itoa(jobCount())); err != nil {
		return err
	}
	fmt.Fprintf(log, "%s: installing\n", name)
	return runLogged(log, src, env, "make", "install")
}

// ensureSource builds source name from s, and the sources it needs, each
// into its own cache/build-tools/<key>, and returns that prefix. A prefix
// is named by what went into it, so it is never rebuilt for another
// version or overwritten by one.
func ensureSource(root string, s Set, name string, log io.Writer) (string, error) {
	t, ok := findTool(s.Sources, name)
	if !ok {
		return "", fmt.Errorf("the lockfile lists no source %s", name)
	}
	r := sourceRecipes[name]
	prefix := filepath.Join(root, "cache", "build-tools", sourceKey(s, name))
	marker := filepath.Join(prefix, ".installed")
	product := filepath.Join(prefix, "bin", name)
	if r.kind == kindLibusb {
		product = filepath.Join(prefix, "lib", "libusb-1.0.a")
	}
	if installed(marker, product, t.SHA256) {
		return prefix, nil
	}
	var path []string
	for _, need := range r.needs {
		p, err := ensureSource(root, s, need, log)
		if err != nil {
			return "", err
		}
		path = append(path, filepath.Join(p, "bin"))
	}
	arc, err := installArchive(root, t, log)
	if err != nil {
		return "", err
	}
	if err := os.RemoveAll(prefix); err != nil {
		return "", err
	}
	var args []string
	if r.kind == kindLibusb {
		args = []string{"--disable-shared", "--enable-static"}
		if runtime.GOOS == "linux" {
			args = append(args, "--disable-udev")
		}
	}
	if err := buildFromSource(root, arc, t.SHA256, prefix, path, args, log); err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	if _, err := os.Stat(product); err != nil {
		return "", fmt.Errorf("source %s: %s was not installed", name, product)
	}
	return prefix, copyToMode(strings.NewReader(t.SHA256+"\n"), marker, 0o644)
}

// sourceBins are the bin directories of names and the sources they need.
func sourceBins(root string, s Set, names []string, log io.Writer) (map[string]string, []string, error) {
	prefixes := map[string]string{}
	var bins []string
	for _, name := range closure(names) {
		p, err := ensureSource(root, s, name, log)
		if err != nil {
			return nil, nil, err
		}
		prefixes[name] = p
		bins = append(bins, filepath.Join(p, "bin"))
	}
	return prefixes, bins, nil
}

func buildDTC(root string, s Set, archive, sum, bin string, log io.Writer) error {
	prefixes, bins, err := sourceBins(root, s, toolRecipes["dtc"].needs, log)
	if err != nil {
		return err
	}
	src, err := extractSource(root, archive, sum)
	if err != nil {
		return err
	}
	fmt.Fprintf(log, "dtc: building\n")
	lex := filepath.Join(prefixes["flex"], "bin", "flex")
	bison := filepath.Join(prefixes["bison"], "bin", "bison")
	if err := runLogged(log, src, buildEnv(root, bins), "make", dtcMakeArgs(jobCount(), lex, bison)...); err != nil {
		return err
	}
	return copyFile(filepath.Join(src, "dtc"), filepath.Join(bin, "dtc"), 0o755)
}

// dtcMakeArgs builds dtc alone. NO_YAML=1 drops libyaml, but the Makefile
// still runs $(PKG_CONFIG), which fails on hosts without pkg-config;
// pointing it at false avoids shipping one.
func dtcMakeArgs(jobs int, lex, bison string) []string {
	return []string{
		"-j" + strconv.Itoa(jobs),
		"dtc",
		"LEX=" + lex,
		"BISON=" + bison,
		"NO_YAML=1",
		"PKG_CONFIG=/usr/bin/false",
	}
}

// buildEnv is the whole environment of a source build. Nothing comes from
// the user's: their PATH, CC, CFLAGS, LDFLAGS and PKG_CONFIG_PATH would
// make builds differ between machines, and HOME and TMPDIR stay under the
// root so a build writes nowhere else. DEVELOPER_DIR is kept, since it
// picks the Xcode command line tools on a Mac.
func buildEnv(root string, extraPATH []string) []string {
	home := filepath.Join(root, "cache", "build-home")
	tmp := filepath.Join(root, "cache", "tmp")
	_ = os.MkdirAll(home, 0o755)
	_ = os.MkdirAll(tmp, 0o755)
	path := append([]string(nil), extraPATH...)
	var dev developerTools
	if runtime.GOOS == "darwin" {
		dev = darwinDeveloperTools()
		path = append(path, dev.bins...)
	}
	path = append(path, "/usr/bin", "/bin", "/usr/sbin", "/sbin")
	env := []string{
		"PATH=" + strings.Join(path, string(os.PathListSeparator)),
		"HOME=" + home,
		"TMPDIR=" + tmp,
		"LANG=C",
		"LC_ALL=C",
		"SOURCE_DATE_EPOCH=0",
	}
	if v := os.Getenv("DEVELOPER_DIR"); v != "" {
		env = append(env, "DEVELOPER_DIR="+v)
	}
	if dev.sdkroot != "" {
		env = append(env, "SDKROOT="+dev.sdkroot)
	}
	return append(env, dev.env...)
}

// developerTools are the parts of a Mac's developer tools a build runs
// directly: their bin directories, the SDK, and the compiler variables
// that name them.
type developerTools struct {
	bins    []string
	sdkroot string
	env     []string
}

// darwinDeveloperTools finds the compiler, make and SDK that /usr/bin/cc
// would use, so a build runs them directly. The /usr/bin shims, and the
// developer tools' own gcc, cpp and m4 wrappers, run xcrun, which writes its
// cache to the per-user temp dir whatever TMPDIR says; configure tries gcc
// before cc, so the compiler is named outright. Without the developer tools
// it finds nothing, and the shims are used.
var darwinDeveloperTools = sync.OnceValue(func() developerTools {
	dev := os.Getenv("DEVELOPER_DIR")
	if dev == "" {
		out, err := exec.Command("/usr/bin/xcode-select", "-p").Output()
		if err != nil {
			return developerTools{}
		}
		dev = strings.TrimSpace(string(out))
	}
	var t developerTools
	for _, dir := range []string{
		filepath.Join(dev, "Toolchains", "XcodeDefault.xctoolchain", "usr", "bin"), // Xcode's compiler
		filepath.Join(dev, "usr", "bin"),                                           // Xcode's make; all of the Command Line Tools
	} {
		if _, err := os.Stat(filepath.Join(dir, "clang")); err == nil && t.env == nil {
			t.env = []string{
				"CC=" + filepath.Join(dir, "clang"),
				"CXX=" + filepath.Join(dir, "clang++"),
				"CPP=" + filepath.Join(dir, "clang") + " -E",
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "gm4")); err == nil {
			t.env = append(t.env, "M4="+filepath.Join(dir, "gm4"))
		}
		if _, err := os.Stat(dir); err == nil {
			t.bins = append(t.bins, dir)
		}
	}
	for _, sdk := range []string{
		filepath.Join(dev, "SDKs", "MacOSX.sdk"),
		filepath.Join(dev, "Platforms", "MacOSX.platform", "Developer", "SDKs", "MacOSX.sdk"),
	} {
		if _, err := os.Stat(sdk); err == nil {
			t.sdkroot = sdk
			break
		}
	}
	return t
})

// recordCompiler writes the first line of cc --version, as a source build
// sees it, to path. It is a record for windlock doctor, so a failure to
// run cc only leaves it out.
func recordCompiler(root, path string) {
	env := buildEnv(root, nil)
	cc, err := lookPathIn("cc", env)
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "CC="); ok {
			cc, err = v, nil
		}
	}
	if err != nil {
		return
	}
	cmd := exec.Command(cc, "--version")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return
	}
	_ = copyToMode(strings.NewReader(firstLine(string(out))+"\n"), path, 0o644)
}

func runLogged(log io.Writer, dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
		// exec resolves name with this process's PATH; use the build's.
		if !strings.ContainsRune(name, os.PathSeparator) {
			if p, err := lookPathIn(name, env); err == nil {
				cmd.Path = p
			}
		}
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

// lookPathIn finds name on the PATH in env.
func lookPathIn(name string, env []string) (string, error) {
	for _, kv := range env {
		if dirs, ok := strings.CutPrefix(kv, "PATH="); ok {
			for _, dir := range filepath.SplitList(dirs) {
				p := filepath.Join(dir, name)
				if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
					return p, nil
				}
			}
		}
	}
	return "", exec.ErrNotFound
}

func jobCount() int {
	n := runtime.NumCPU()
	if n < 1 {
		return 1
	}
	return n
}
