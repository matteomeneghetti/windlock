package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// installSDK installs SDK version with toolchains through the workspace's
// west sdk install, adding only what the install directory lacks.
func installSDK(h hostEnv, w workspace, version string, toolchains []string) error {
	log, root := h.log, h.root
	installBase, sdkDir := h.sdkDirs(version)
	want := append([]string(nil), toolchains...)
	if SDKInstalled(sdkDir, version) {
		want = missingToolchains(sdkDir, toolchains)
		if len(want) == 0 {
			fmt.Fprintf(log, "sdk already installed at %s\n", sdkDir)
			return nil
		}
	}
	if err := os.MkdirAll(installBase, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(root, "cache", "tmp")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	args := []string{"sdk", "install", "--version", version, "--install-base", installBase}
	if len(want) == 0 {
		args = append(args, "--no-gnu-toolchains")
	} else {
		args = append(args, "-t")
		args = append(args, want...)
	}
	fmt.Fprintf(log, "west %s\n", strings.Join(args, " "))
	text, err := runCapture(log, w.dir, childEnv(h.hostBin, w.venv, root, tmp), w.west, args...)
	if err != nil {
		if !SDKInstalled(sdkDir, version) {
			if rmErr := removeIncompleteSDK(installBase, version); rmErr != nil {
				return rmErr
			}
		}
		if isRateLimit(text) {
			return fmt.Errorf("GitHub rate limit during west sdk install; wait and retry: %w", err)
		}
		return fmt.Errorf("west sdk install: %w", err)
	}
	return nil
}

func removeIncompleteSDK(installBase, version string) error {
	sdkDir := filepath.Join(installBase, "zephyr-sdk-"+version)
	if SDKInstalled(sdkDir, version) {
		return nil
	}
	return os.RemoveAll(installBase)
}

func missingToolchains(sdkDir string, toolchains []string) []string {
	var missing []string
	for _, tc := range toolchains {
		if !toolchainInstalled(sdkDir, tc) {
			missing = append(missing, tc)
		}
	}
	return missing
}

// toolchainInstalled reports whether a GNU toolchain is present. SDK 0.17
// keeps it at the install root. SDK 1.0.1 keeps it under gnu/.
func toolchainInstalled(sdkDir, tc string) bool {
	for _, path := range []string{
		filepath.Join(sdkDir, tc),
		filepath.Join(sdkDir, "gnu", tc),
	} {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// SDKInstalled reports whether dir holds the SDK release version.
func SDKInstalled(dir, version string) bool {
	got, err := readFirstLine(filepath.Join(dir, "sdk_version"))
	return err == nil && got == version
}

func readFirstLine(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(b))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if line == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return line, nil
}
