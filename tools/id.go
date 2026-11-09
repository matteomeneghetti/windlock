package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/matteomeneghetti/windlock/host"
)

// Version is one pinned tool version. The host-tools id hashes these and
// does not include URLs, checksums, or the host triple.
type Version struct {
	Name    string
	Version string
}

// ID is the hex sha256 of the pinned versions. Names are sorted, and each
// line is "name version\n". The same versions always produce the same id.
func ID(vs []Version) string {
	cp := append([]Version(nil), vs...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Name < cp[j].Name })
	var b strings.Builder
	for _, v := range cp {
		fmt.Fprintf(&b, "%s %s\n", v.Name, v.Version)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// CurrentID is the host-tools id for this process's triple.
func CurrentID() (string, error) {
	triple, err := host.Current()
	if err != nil {
		return "", err
	}
	return IDFor(triple)
}

// IDFor is the host-tools id for triple. Triples that pin the same versions
// share an id.
func IDFor(triple string) (string, error) {
	pins, err := HostPins(triple)
	if err != nil {
		return "", err
	}
	return ID(versionsOf(pins)), nil
}
