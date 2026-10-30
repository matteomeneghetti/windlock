// Package host maps the running GOOS/GOARCH to a Zephyr SDK host triple.
package host

import (
	"fmt"
	"runtime"
	"strings"
)

// Triples are the host triples a lockfile records: Linux ones for the
// container, which builds, and macOS ones for the probe side.
var Triples = []string{"macos-aarch64", "macos-x86_64", "linux-x86_64", "linux-aarch64"}

// Builds reports whether the container runs as triple, so its platform
// needs SDK bundles.
func Builds(triple string) bool { return strings.HasPrefix(triple, "linux-") }

// Detect maps a GOOS/GOARCH pair to one of Triples.
func Detect(goos, goarch string) (string, error) {
	osName, okOS := map[string]string{"darwin": "macos", "linux": "linux"}[goos]
	archName, okArch := map[string]string{"arm64": "aarch64", "amd64": "x86_64"}[goarch]
	if !okOS || !okArch {
		return "", fmt.Errorf("unsupported host: GOOS=%q GOARCH=%q", goos, goarch)
	}
	return osName + "-" + archName, nil
}

// Current detects the triple of this binary; a darwin/amd64 build reports
// macos-x86_64 even under Rosetta.
func Current() (string, error) {
	return Detect(runtime.GOOS, runtime.GOARCH)
}
