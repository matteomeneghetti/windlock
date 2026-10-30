package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ensureWestWorkspace makes ws a west workspace whose zephyr, cloned from
// spec.url with westBin, is at spec.commit.
func ensureWestWorkspace(h hostEnv, ws string, spec entrySpec, westBin string) error {
	if head, err := revParse(filepath.Join(ws, "zephyr")); err == nil && head == spec.commit {
		fmt.Fprintf(h.log, "workspace %s\n", ws)
		return nil
	}
	if _, err := os.Stat(ws); err == nil {
		return fmt.Errorf("workspace %s exists but zephyr HEAD is not %s", ws, spec.commit)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ws), 0o755); err != nil {
		return err
	}
	fmt.Fprintf(h.log, "west init %s\n", ws)
	err := runStreaming(h.log, "", childEnv(h.hostBin, "", h.root, ""), westBin,
		"init", "-m", spec.url, "--mr", spec.revision, ws)
	if err != nil {
		_ = os.RemoveAll(ws)
		return fmt.Errorf("west init: %w", err)
	}
	head, err := revParse(filepath.Join(ws, "zephyr"))
	if err != nil {
		_ = os.RemoveAll(ws)
		return err
	}
	if head != spec.commit {
		_ = os.RemoveAll(ws)
		return fmt.Errorf("west init checked out %s, want %s", head, spec.commit)
	}
	return nil
}

// lookupCommit resolves a tag or branch of repo to a commit with one
// git ls-remote. A tag wins over a branch of the same name.
func lookupCommit(repo, rev string) (string, error) {
	out, err := gitOutput("ls-remote", repo, rev, rev+"^{}")
	if err != nil {
		return "", err
	}
	commit, err := commitFromLSRemote(out, rev)
	if err != nil {
		return "", fmt.Errorf("resolve %s in %s: %w", rev, repo, err)
	}
	return commit, nil
}

func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

func revParse(repo string) (string, error) {
	cmd := exec.Command("git", "-C", repo, "rev-parse", "HEAD")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
