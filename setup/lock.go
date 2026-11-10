package setup

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/matteomeneghetti/windlock/host"
	"github.com/matteomeneghetti/windlock/lockfile"
	"github.com/matteomeneghetti/windlock/tools"
)

// pinSection is the part of a lockfile that pins tools: the container
// image, the sources, and each platform's build and flash tools. init takes
// it from this windlock's defaults; every other command that writes the
// lockfile keeps the locked one, so pins change only when init runs.
type pinSection struct {
	image   string
	sources map[string]lockfile.Tool
	build   map[string]map[string]lockfile.Tool
	flash   map[string]map[string]lockfile.Tool
}

// defaultPins are this windlock's pins, for init to lock.
func defaultPins() (pinSection, error) {
	ps := pinSection{
		image:   tools.ContainerImage,
		sources: toLockTools(tools.DefaultSources()),
		build:   map[string]map[string]lockfile.Tool{},
		flash:   map[string]map[string]lockfile.Tool{},
	}
	for _, triple := range host.Triples {
		if b, err := tools.DefaultBuildTools(triple); err == nil {
			ps.build[triple] = toLockTools(b)
		}
		f, err := tools.DefaultFlashTools(triple)
		if err != nil {
			return pinSection{}, err
		}
		ps.flash[triple] = toLockTools(f)
	}
	return ps, nil
}

// lockedPins are the pins lock already records.
func lockedPins(lock lockfile.Lockfile) pinSection {
	ps := pinSection{
		image:   lock.Container.Image,
		sources: lock.Sources,
		build:   map[string]map[string]lockfile.Tool{},
		flash:   map[string]map[string]lockfile.Tool{},
	}
	for triple, p := range lock.Platforms {
		if len(p.BuildTools) > 0 {
			ps.build[triple] = p.BuildTools
		}
		if len(p.FlashTools) > 0 {
			ps.flash[triple] = p.FlashTools
		}
	}
	return ps
}

// assembleLock builds the lockfile for entries from pins and each SDK's
// sha256.sum, identically on every host; current must be able to use it.
func assembleLock(entries []lockfile.Zephyr, westVersion string, sums map[string]string, pins pinSection, current string) (lockfile.Lockfile, error) {
	if len(entries) == 0 {
		return lockfile.Lockfile{}, fmt.Errorf("no zephyr entry to lock")
	}
	if strings.TrimSpace(westVersion) == "" {
		return lockfile.Lockfile{}, fmt.Errorf("west version is empty")
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if err := checkEntry(e); err != nil {
			return lockfile.Lockfile{}, err
		}
		if seen[e.Name] {
			return lockfile.Lockfile{}, fmt.Errorf("zephyr %s is listed twice", e.Name)
		}
		seen[e.Name] = true
	}
	if pins.image == "" {
		return lockfile.Lockfile{}, fmt.Errorf("no container image to lock")
	}
	lock := lockfile.Lockfile{
		Schema:    lockfile.Schema,
		West:      westVersion,
		Container: lockfile.Container{Image: pins.image},
		Sources:   pins.sources,
		Zephyr:    entries,
		Platforms: map[string]lockfile.Platform{},
	}
	if lock.Sources == nil {
		lock.Sources = map[string]lockfile.Tool{}
	}
	for _, triple := range host.Triples {
		p := lockfile.Platform{BuildTools: pins.build[triple], FlashTools: pins.flash[triple]}
		for _, version := range lock.SDKs() {
			if !host.Builds(triple) {
				break
			}
			text, ok := sums[version]
			if !ok {
				return lockfile.Lockfile{}, fmt.Errorf("no sha256.sum for SDK %s", version)
			}
			name, sum, err := selectMinimalBundle(text, version, triple)
			if errors.Is(err, errNoBundle) {
				continue
			}
			if err != nil {
				return lockfile.Lockfile{}, err
			}
			if p.SDK == nil {
				p.SDK = map[string]lockfile.Bundle{}
			}
			p.SDK[version] = lockfile.Bundle{Filename: name, URL: sdkReleaseURL(version, name), SHA256: sum}
		}
		if len(p.BuildTools) > 0 || len(p.FlashTools) > 0 || len(p.SDK) > 0 {
			lock.Platforms[triple] = p
		}
	}
	for triple, p := range lock.Platforms {
		sets := map[string]map[string]lockfile.Tool{"build": p.BuildTools, "flash": p.FlashTools}
		for kind, ts := range sets {
			if len(ts) == 0 {
				continue
			}
			if err := tools.Validate(tools.Set{Tools: fromLockTools(ts), Sources: fromLockTools(lock.Sources)}); err != nil {
				return lockfile.Lockfile{}, fmt.Errorf("%s %s tools: %w", triple, kind, err)
			}
		}
	}
	if err := usablePlatform(lock, current); err != nil {
		return lockfile.Lockfile{}, err
	}
	return lock, nil
}

// BuildSet is what the container installs on triple to build: the
// platform's build tools and the sources their recipes need.
func BuildSet(lock lockfile.Lockfile, triple string) (tools.Set, error) {
	p, err := lock.Platform(triple)
	if err != nil {
		return tools.Set{}, err
	}
	if len(p.BuildTools) == 0 {
		return tools.Set{}, fmt.Errorf("%s has no build tools for %s; rerun windlock init", lockfile.Filename, triple)
	}
	return tools.Set{Tools: fromLockTools(p.BuildTools), Sources: fromLockTools(lock.Sources)}, nil
}

// FlashSet is what a host with the probe installs on triple to flash and
// debug: the platform's flash tools and the sources their recipes need.
func FlashSet(lock lockfile.Lockfile, triple string) (tools.Set, error) {
	p, err := lock.Platform(triple)
	if err != nil {
		return tools.Set{}, err
	}
	if len(p.FlashTools) == 0 {
		return tools.Set{}, fmt.Errorf("%s has no flash tools for %s; rerun windlock init", lockfile.Filename, triple)
	}
	return tools.Set{Tools: fromLockTools(p.FlashTools), Sources: fromLockTools(lock.Sources)}, nil
}

func toLockTools(ts []tools.Tool) map[string]lockfile.Tool {
	out := make(map[string]lockfile.Tool, len(ts))
	for _, t := range ts {
		out[t.Name] = lockfile.Tool{Version: t.Version, URL: t.URL, SHA256: t.SHA256}
	}
	return out
}

// fromLockTools lists m's tools by name, so installs run in a fixed order.
func fromLockTools(m map[string]lockfile.Tool) []tools.Tool {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]tools.Tool, 0, len(m))
	for _, name := range names {
		t := m[name]
		out = append(out, tools.Tool{Name: name, Version: t.Version, URL: t.URL, SHA256: t.SHA256})
	}
	return out
}

func checkEntry(e lockfile.Zephyr) error {
	if e.Name == "" || e.Revision == "" || e.URL == "" {
		return fmt.Errorf("zephyr entry needs a name, revision, and url")
	}
	if !isHex(e.Commit, 40) {
		return fmt.Errorf("zephyr %s commit %q is not 40 hex digits", e.Name, e.Commit)
	}
	if !isHex(e.ManifestSHA256, 64) {
		return fmt.Errorf("zephyr %s manifest sha256 %q is not 64 hex digits", e.Name, e.ManifestSHA256)
	}
	if e.SDK == "" {
		return fmt.Errorf("zephyr %s sdk version is empty", e.Name)
	}
	if !isHex(e.PythonInputsSHA256, 64) || len(e.PythonRequirements) == 0 {
		return fmt.Errorf("zephyr %s has no pinned Python packages", e.Name)
	}
	return nil
}

// usablePlatform fails when triple cannot build from lock: it has no build
// tools, or no bundle for an SDK an entry uses.
func usablePlatform(lock lockfile.Lockfile, triple string) error {
	if _, err := BuildSet(lock, triple); err != nil {
		return err
	}
	p, _ := lock.Platform(triple)
	for _, version := range lock.SDKs() {
		if _, ok := p.SDK[version]; !ok {
			return fmt.Errorf("%s has no SDK %s bundle for host %s (recorded hosts: %s)",
				lockfile.Filename, version, triple, strings.Join(sdkHosts(lock, version), ", "))
		}
	}
	return nil
}

func sdkHosts(lock lockfile.Lockfile, version string) []string {
	var out []string
	for triple, p := range lock.Platforms {
		if _, ok := p.SDK[version]; ok {
			out = append(out, triple)
		}
	}
	if len(out) == 0 {
		return []string{"none"}
	}
	sort.Strings(out)
	return out
}

// HostToolsBin is the build-tools bin directory lock selects for triple.
func HostToolsBin(lock lockfile.Lockfile, root, triple string) (string, error) {
	set, err := BuildSet(lock, triple)
	if err != nil {
		return "", err
	}
	return tools.BinDirFor(root, set), nil
}

func unionStrings(a, b []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range append(append([]string(nil), a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
