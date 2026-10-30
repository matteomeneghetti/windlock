package setup

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"windlock/host"
	"windlock/lockfile"
	"windlock/tools"
)

// assembleLock builds the lockfile for entries from the pins and each SDK's
// sha256.sum, identically on every host; current must be able to use it.
func assembleLock(entries []lockfile.Zephyr, westVersion string, sums map[string]string, current string) (lockfile.Lockfile, error) {
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
	lock := lockfile.Lockfile{
		Schema:    lockfile.Schema,
		West:      westVersion,
		Container: lockfile.Container{Image: tools.ContainerImage},
		HostTools: map[string]string{},
		Zephyr:    entries,
		Platforms: map[string]lockfile.Platform{},
	}
	for _, triple := range host.Triples {
		p := lockfile.Platform{}
		if pins, err := tools.HostPins(triple); err == nil {
			p.HostTools = map[string]lockfile.Archive{}
			for _, a := range pins {
				if have, ok := lock.HostTools[a.Name]; ok && have != a.Version {
					return lockfile.Lockfile{}, fmt.Errorf("host tool %s is %s on %s and %s on another host", a.Name, a.Version, triple, have)
				}
				if a.Version == "" || a.URL == "" || !isHex(a.SHA256, 64) {
					return lockfile.Lockfile{}, fmt.Errorf("host tool %s on %s has no pinned url and sha256", a.Name, triple)
				}
				lock.HostTools[a.Name] = a.Version
				p.HostTools[a.Name] = lockfile.Archive{URL: a.URL, SHA256: a.SHA256}
			}
		}
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
		if len(p.HostTools) > 0 || len(p.SDK) > 0 {
			lock.Platforms[triple] = p
		}
	}
	if err := usablePlatform(lock, current); err != nil {
		return lockfile.Lockfile{}, err
	}
	return lock, nil
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

// usablePlatform fails when triple cannot install from lock: it has no
// installable host tools, or no bundle for an SDK an entry uses.
func usablePlatform(lock lockfile.Lockfile, triple string) error {
	p, err := lock.Platform(triple)
	if err != nil {
		return err
	}
	if len(p.HostTools) == 0 {
		return fmt.Errorf("%s has no host tools for %s", lockfile.Filename, triple)
	}
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

// checkHostTools fails when the lockfile's host tools for triple are not the
// pins this windlock carries, which is the set Install would put on disk.
func checkHostTools(lock lockfile.Lockfile, triple string, pins []tools.Archive) error {
	p, err := lock.Platform(triple)
	if err != nil {
		return err
	}
	if len(p.HostTools) != len(pins) {
		return fmt.Errorf("%s records %d host tools for %s, this windlock pins %d; use the windlock build that wrote the lockfile, or rerun windlock init",
			lockfile.Filename, len(p.HostTools), triple, len(pins))
	}
	for _, pin := range pins {
		got, ok := p.HostTools[pin.Name]
		if !ok {
			return fmt.Errorf("%s has no %s archive for host %s", lockfile.Filename, pin.Name, triple)
		}
		if v := lock.HostTools[pin.Name]; v != pin.Version {
			return fmt.Errorf("%s pins %s %s, this windlock pins %s", lockfile.Filename, pin.Name, v, pin.Version)
		}
		if got.URL != pin.URL || !strings.EqualFold(got.SHA256, pin.SHA256) {
			return fmt.Errorf("%s %s for %s is %s %s, this windlock pins %s %s",
				lockfile.Filename, pin.Name, triple, got.URL, got.SHA256, pin.URL, pin.SHA256)
		}
	}
	return nil
}

// hostToolsID is the host-tools directory id for triple: the hash of the
// versions of the programs its platform records.
func hostToolsID(lock lockfile.Lockfile, triple string) (string, error) {
	p, err := lock.Platform(triple)
	if err != nil {
		return "", err
	}
	var vs []tools.Version
	for name := range p.HostTools {
		v, ok := lock.HostTools[name]
		if !ok {
			return "", fmt.Errorf("%s has no %s version", lockfile.Filename, name)
		}
		vs = append(vs, tools.Version{Name: name, Version: v})
	}
	return tools.ID(vs), nil
}

// HostToolsBin is the host-tools bin directory lock selects for triple.
func HostToolsBin(lock lockfile.Lockfile, root, triple string) (string, error) {
	id, err := hostToolsID(lock, triple)
	if err != nil {
		return "", err
	}
	return tools.BinDir(root, id), nil
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
