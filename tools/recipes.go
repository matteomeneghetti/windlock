package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
)

// Tool is one locked download, as the lockfile records it: SHA256 is the
// checksum of the bytes at URL.
type Tool struct {
	Name    string
	Version string
	URL     string
	SHA256  string
}

// Set is what one install location installs: its tools, and the source
// tarballs their recipes build with.
type Set struct {
	Tools   []Tool
	Sources []Tool
}

// Kind selects how an archive is installed.
type Kind int

const (
	kindCMake Kind = iota
	kindNinja
	kindPython
	kindGperf
	kindDTC
	kindAutotools
	kindUV
	kindOpenOCD
	kindDFUUtil
	kindLibusb
)

// recipe is how windlock installs one tool or source, whatever version the
// lockfile names. needs are the sources it builds with. Bump revision when
// a change to the recipe changes what it installs, so no install made by
// the old recipe is reused.
type recipe struct {
	kind     Kind
	needs    []string
	revision int
}

var toolRecipes = map[string]recipe{
	"cmake":    {kind: kindCMake, revision: 1},
	"ninja":    {kind: kindNinja, revision: 1},
	"python":   {kind: kindPython, revision: 1},
	"uv":       {kind: kindUV, revision: 1},
	"gperf":    {kind: kindGperf, revision: 1},
	"dtc":      {kind: kindDTC, needs: []string{"flex", "bison"}, revision: 1},
	"openocd":  {kind: kindOpenOCD, revision: 1},
	"dfu-util": {kind: kindDFUUtil, needs: []string{"libusb"}, revision: 1},
}

var sourceRecipes = map[string]recipe{
	"m4":     {kind: kindAutotools, revision: 1},
	"flex":   {kind: kindAutotools, needs: []string{"m4"}, revision: 1},
	"bison":  {kind: kindAutotools, needs: []string{"m4"}, revision: 1},
	"libusb": {kind: kindLibusb, revision: 1},
}

// Validate fails unless windlock can install every tool in s: each has a
// recipe, a version, an https URL and a sha256, and every source a recipe
// needs is in s.
func Validate(s Set) error {
	seen := map[string]bool{}
	for _, t := range s.Tools {
		if _, ok := toolRecipes[t.Name]; !ok {
			return fmt.Errorf("no install recipe for tool %s; upgrade windlock", t.Name)
		}
		if seen[t.Name] {
			return fmt.Errorf("tool %s is listed twice", t.Name)
		}
		seen[t.Name] = true
		if err := checkTool(t); err != nil {
			return err
		}
	}
	sources := map[string]bool{}
	for _, t := range s.Sources {
		if _, ok := sourceRecipes[t.Name]; !ok {
			return fmt.Errorf("no build recipe for source %s; upgrade windlock", t.Name)
		}
		if err := checkTool(t); err != nil {
			return err
		}
		sources[t.Name] = true
	}
	for _, t := range s.Tools {
		for _, need := range closure(toolRecipes[t.Name].needs) {
			if !sources[need] {
				return fmt.Errorf("%s builds with source %s, which the lockfile does not list", t.Name, need)
			}
		}
	}
	return nil
}

func checkTool(t Tool) error {
	if t.Version == "" {
		return fmt.Errorf("%s has no version", t.Name)
	}
	if err := requireHTTPS(t.URL); err != nil {
		return fmt.Errorf("%s: %w", t.Name, err)
	}
	if len(t.SHA256) != 64 || strings.Trim(strings.ToLower(t.SHA256), "0123456789abcdef") != "" {
		return fmt.Errorf("%s sha256 %q is not 64 hex digits", t.Name, t.SHA256)
	}
	if fileName(t) == "" {
		return fmt.Errorf("%s url %s names no file", t.Name, t.URL)
	}
	return nil
}

// closure is names and every source they need, transitively, sorted.
func closure(names []string) []string {
	seen := map[string]bool{}
	var visit func(string)
	visit = func(n string) {
		if seen[n] {
			return
		}
		seen[n] = true
		for _, m := range sourceRecipes[n].needs {
			visit(m)
		}
	}
	for _, n := range names {
		visit(n)
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ID names the host-tools directory for s: the hash of each tool's name,
// sha256 and recipe revision, and the key of every source it builds with.
// A new archive, source or recipe gets a new directory.
func ID(s Set) string {
	tools := append([]Tool(nil), s.Tools...)
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	var b strings.Builder
	for _, t := range tools {
		r := toolRecipes[t.Name]
		fmt.Fprintf(&b, "%s %s %d", t.Name, strings.ToLower(t.SHA256), r.revision)
		for _, need := range r.needs {
			fmt.Fprintf(&b, " %s", sourceKey(s, need))
		}
		b.WriteByte('\n')
	}
	return hashHex(b.String())
}

// sourceKey names the build of source name in s: its sha256, recipe
// revision, and the keys of the sources it builds with.
func sourceKey(s Set, name string) string {
	t, _ := findTool(s.Sources, name)
	r := sourceRecipes[name]
	line := fmt.Sprintf("%s %s %d", name, strings.ToLower(t.SHA256), r.revision)
	for _, need := range r.needs {
		line += " " + sourceKey(s, need)
	}
	return hashHex(line)
}

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func findTool(ts []Tool, name string) (Tool, bool) {
	for _, t := range ts {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// fileName is the name the download is cached under: the URL's last path
// element.
func fileName(t Tool) string {
	u, err := url.Parse(t.URL)
	if err != nil {
		return ""
	}
	base := path.Base(u.Path)
	if base == "/" || base == "." {
		return ""
	}
	return base
}
