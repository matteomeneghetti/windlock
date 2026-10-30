package setup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// keepLogs is how many command logs stay in <root>/logs.
const keepLogs = 10

// Output is the setup commands' log: progress goes to the terminal, child
// output to <root>/logs only (or both when verbose).
type Output struct {
	progress io.Writer
	verbose  bool
	path     string
	file     *os.File
	err      error
}

// NewOutput writes progress to w, and child output to
// <root>/logs/<command>-<time>.log, which is created on first use.
func NewOutput(w io.Writer, root, command string, verbose bool) *Output {
	name := fmt.Sprintf("%s-%s-%d.log", command, time.Now().Format("20060102-150405"), os.Getpid())
	return &Output{progress: w, verbose: verbose, path: filepath.Join(root, "logs", name)}
}

// Write is a progress line: shown, and recorded in the log for context.
func (o *Output) Write(p []byte) (int, error) {
	if f := o.open(); f != nil {
		_, _ = f.Write(p)
	}
	return o.progress.Write(p)
}

// child is where a child's output goes.
func (o *Output) child() io.Writer {
	f := o.open()
	switch {
	case o.verbose && f != nil:
		return io.MultiWriter(o.progress, f)
	case o.verbose || f == nil:
		// Without a log file, hiding the output would lose it.
		return o.progress
	default:
		return f
	}
}

func (o *Output) open() *os.File {
	if o.file != nil || o.err != nil {
		return o.file
	}
	if o.err = os.MkdirAll(filepath.Dir(o.path), 0o755); o.err != nil {
		return nil
	}
	o.file, o.err = os.Create(o.path)
	return o.file
}

// Close closes the log file and keeps only the newest logs.
func (o *Output) Close() error {
	if o.file == nil {
		return nil
	}
	err := o.file.Close()
	pruneLogs(filepath.Dir(o.path), keepLogs)
	return err
}

// failure describes a child that failed: in quiet mode with its last lines
// and the log path, since nothing of it reached the terminal.
func (o *Output) failure(name string, err error, text string) error {
	if o.verbose || o.file == nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return fmt.Errorf("%s: %w\n%s\nfull output: %s", name, err, lastLines(text, 20), o.path)
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func pruneLogs(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var logs []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			logs = append(logs, e.Name())
		}
	}
	// Names end in a sortable timestamp; sort by it, not by command.
	sort.Slice(logs, func(i, j int) bool { return stamp(logs[i]) < stamp(logs[j]) })
	for len(logs) > keep {
		_ = os.Remove(filepath.Join(dir, logs[0]))
		logs = logs[1:]
	}
}

var stampRE = regexp.MustCompile(`\d{8}-\d{6}`)

// stamp is the date-time in a log name, so logs of every command sort together.
func stamp(name string) string {
	return stampRE.FindString(name)
}
