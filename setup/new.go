package setup

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/matteomeneghetti/windlock/lockfile"
)

//go:embed templates
var templates embed.FS

// newSteps are what New runs after writing the files, so tests can stand
// in for the network.
type newSteps struct {
	init      func(appDir, revision string, log io.Writer) error
	targetAdd func(appDir, name, board, source string, opts TargetOptions, log io.Writer) error
}

var realNewSteps = newSteps{init: Init, targetAdd: TargetAdd}

// New creates a project with one app in apps/<name> in a new or empty dir;
// with board it also runs init and adds the app as the default target.
func New(dir, name, board, zephyr string, log io.Writer) error {
	return realNewSteps.run(dir, name, board, zephyr, log)
}

// NewWith is New with init and target add replaced, so the container
// backend can run them in the container.
func NewWith(dir, name, board, zephyr string, log io.Writer,
	init func(appDir, revision string, log io.Writer) error,
	targetAdd func(appDir, name, board, source string, opts TargetOptions, log io.Writer) error) error {
	return newSteps{init: init, targetAdd: targetAdd}.run(dir, name, board, zephyr, log)
}

func (s newSteps) run(dir, name, board, zephyr string, log io.Writer) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if name == "" {
		name = filepath.Base(dir)
	}
	if err := lockfile.ValidTargetName(name); err != nil {
		return fmt.Errorf("application name: %w; pick one with --name", err)
	}
	if err := requireEmptyDir(dir); err != nil {
		return err
	}
	files, err := renderSkeleton(name)
	if err != nil {
		return err
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, content, 0o644); err != nil {
			return err
		}
	}
	source := "apps/" + name
	fmt.Fprintf(log, "created %s with application %s\n", dir, source)
	if board == "" {
		initCmd := "windlock init"
		if zephyr != "" {
			initCmd += " --zephyr " + zephyr
		}
		fmt.Fprintf(log, "next:\n  cd %s\n  %s\n  windlock target add %s --board <board> --source %s\n  windlock build\n",
			dir, initCmd, name, source)
		return nil
	}
	if err := s.init(dir, zephyr, log); err != nil {
		return err
	}
	if err := s.targetAdd(dir, name, board, source, TargetOptions{Default: true}, log); err != nil {
		return fmt.Errorf("%s is created and initialised, but its target was not added: %w\nfix the board, then run in %s: windlock target add %s --board <board> --source %s",
			dir, err, dir, name, source)
	}
	fmt.Fprintf(log, "ready: cd %s && windlock build\n", dir)
	return nil
}

// requireEmptyDir passes for a missing directory or an empty one.
func requireEmptyDir(dir string) error {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s exists and is not a directory", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s is not empty; windlock new only creates projects in a new or empty directory", dir)
	}
	return nil
}

// renderSkeleton renders the templates by project path: app/... becomes
// apps/<name>/..., and gitignore becomes .gitignore.
func renderSkeleton(name string) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := fs.WalkDir(templates, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimSuffix(strings.TrimPrefix(p, "templates/"), ".tmpl")
		switch {
		case rel == "gitignore":
			rel = ".gitignore"
		case strings.HasPrefix(rel, "app/"):
			rel = path.Join("apps", name, strings.TrimPrefix(rel, "app/"))
		}
		raw, err := templates.ReadFile(p)
		if err != nil {
			return err
		}
		t, err := template.New(p).Option("missingkey=error").Parse(string(raw))
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, struct{ Name string }{name}); err != nil {
			return err
		}
		out[rel] = buf.Bytes()
		return nil
	})
	return out, err
}
