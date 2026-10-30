package setup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ensureVenv(python, venv, root string, log io.Writer) error {
	if _, err := os.Stat(filepath.Join(venv, "bin", "python")); err == nil {
		return nil
	}
	fmt.Fprintf(log, "creating venv %s\n", venv)
	if err := os.MkdirAll(filepath.Dir(venv), 0o755); err != nil {
		return err
	}
	return runStreaming(log, "", childEnv(filepath.Dir(python), "", root, ""), python, "-m", "venv", venv)
}

func pipInstall(venv, root, spec string, log io.Writer) error {
	python := filepath.Join(venv, "bin", "python")
	fmt.Fprintf(log, "pip install %s\n", spec)
	return runStreaming(log, "", childEnv(filepath.Dir(python), venv, root, ""), python, "-m", "pip", "install", spec)
}

func readWestVersion(west string, env []string) (string, error) {
	text, err := runCapture(io.Discard, "", env, west, "--version")
	if err != nil && strings.TrimSpace(text) == "" {
		return "", err
	}
	return parseWestVersion(text)
}
