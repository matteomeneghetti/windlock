package setup

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

var errNoBundle = errors.New("no bundle")

func sdkReleaseURL(version, filename string) string {
	return fmt.Sprintf("https://github.com/zephyrproject-rtos/sdk-ng/releases/download/v%s/%s", version, filename)
}

// minimalBundleName is the minimal SDK archive name for triple. Windows
// bundles are 7z archives; every other host gets a tar.xz.
func minimalBundleName(version, triple string) string {
	ext := ".tar.xz"
	if strings.HasPrefix(triple, "windows-") {
		ext = ".7z"
	}
	return fmt.Sprintf("zephyr-sdk-%s_%s_minimal%s", version, triple, ext)
}

// selectMinimalBundle finds triple's minimal SDK archive in a sha256.sum text;
// a release without one returns an error wrapping errNoBundle.
func selectMinimalBundle(text, version, triple string) (filename, sum string, err error) {
	want := minimalBundleName(version, triple)
	var found string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		hash, name := fields[0], strings.TrimPrefix(fields[1], "*")
		if name != want {
			continue
		}
		hash = strings.ToLower(hash)
		if !isHex(hash, 64) {
			return "", "", fmt.Errorf("sha256.sum hash for %s is not 64 hex digits", want)
		}
		if found != "" && found != hash {
			return "", "", fmt.Errorf("sha256.sum has conflicting hashes for %s", want)
		}
		found = hash
	}
	if found == "" {
		return "", "", fmt.Errorf("sha256.sum has no %s for SDK %s on %s: %w", want, version, triple, errNoBundle)
	}
	return want, found, nil
}

// commitFromLSRemote returns rev's commit from git ls-remote output, preferring
// a peeled tag over the tag object, and a tag over a same-named branch.
func commitFromLSRemote(out, rev string) (string, error) {
	byRef := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !isHex(fields[0], 40) {
			continue
		}
		byRef[fields[1]] = fields[0]
	}
	for _, ref := range []string{"refs/tags/" + rev + "^{}", "refs/tags/" + rev, "refs/heads/" + rev} {
		if commit, ok := byRef[ref]; ok {
			return commit, nil
		}
	}
	return "", fmt.Errorf("no tag or branch %s", rev)
}

func parseWestVersion(out string) (string, error) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(strings.ToLower(line), "version") {
			continue
		}
		fields := strings.Fields(line)
		version := strings.TrimPrefix(fields[len(fields)-1], "v")
		if version != "" && version != "version:" {
			return version, nil
		}
	}
	return "", fmt.Errorf("no west version in %q", strings.TrimSpace(out))
}

func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, r := range s {
		if !unicode.Is(unicode.Hex_Digit, r) {
			return false
		}
	}
	return true
}

func isRateLimit(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "rate limit") || strings.Contains(lower, "api rate")
}
