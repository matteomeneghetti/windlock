package tools

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

const (
	cmakeFloor = "3.20.5"
	dtcFloor   = "1.4.6"
)

var versionRE = regexp.MustCompile(`(\d+)\.(\d+)(?:\.(\d+))?`)

func parseVersion(s string) ([3]int, error) {
	m := versionRE.FindStringSubmatch(s)
	if m == nil {
		return [3]int{}, fmt.Errorf("no version in %q", s)
	}
	var v [3]int
	for i := 1; i < len(m); i++ {
		if m[i] == "" {
			continue
		}
		n, err := strconv.Atoi(m[i])
		if err != nil {
			return [3]int{}, err
		}
		v[i-1] = n
	}
	return v, nil
}

func atLeast(got, floor string) (bool, error) {
	g, err := parseVersion(got)
	if err != nil {
		return false, err
	}
	f, err := parseVersion(floor)
	if err != nil {
		return false, err
	}
	for i := 0; i < 3; i++ {
		if g[i] != f[i] {
			return g[i] > f[i], nil
		}
	}
	return true, nil
}

func requireAtLeast(name, text, floor string) error {
	ok, err := atLeast(text, floor)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if !ok {
		return fmt.Errorf("%s version %q is below %s", name, strings.TrimSpace(text), floor)
	}
	return nil
}

func requirePython312(text string) error {
	v, err := parseVersion(text)
	if err != nil {
		return fmt.Errorf("python: %w", err)
	}
	if v[0] != 3 || v[1] != 12 {
		return fmt.Errorf("python version %d.%d is not 3.12", v[0], v[1])
	}
	return nil
}

func commandOutput(bin string, args ...string) (string, error) {
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w\n%s", bin, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func versionText(bin string) (string, error) {
	text, err := commandOutput(bin, "--version")
	if err == nil {
		if _, perr := parseVersion(text); perr == nil {
			return text, nil
		}
	}
	alt, aerr := commandOutput(bin, "-v")
	if aerr == nil {
		if _, perr := parseVersion(alt); perr == nil {
			return alt, nil
		}
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s: no version in %q", bin, text)
}

// AtLeast reports whether version got is at or above floor.
func AtLeast(got, floor string) (bool, error) {
	return atLeast(got, floor)
}

// CheckPython312 fails unless the interpreter at bin is CPython 3.12.
func CheckPython312(bin string) error {
	text, err := versionText(bin)
	if err != nil {
		return err
	}
	return requirePython312(text)
}
