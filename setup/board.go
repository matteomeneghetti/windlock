package setup

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/matteomeneghetti/windlock/host"
	"github.com/matteomeneghetti/windlock/lockfile"
	"github.com/matteomeneghetti/windlock/tools"
)

// readSDKVersion returns the SDK release named by zephyr/SDK_VERSION.
func readSDKVersion(zephyrDir string) (string, error) {
	return readFirstLine(filepath.Join(zephyrDir, "SDK_VERSION"))
}

// toolchainForArch maps a Zephyr board arch to the GNU toolchain west sdk
// install expects.
func toolchainForArch(arch string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "arm":
		return "arm-zephyr-eabi", nil
	case "x86":
		return "x86_64-zephyr-elf", nil
	case "riscv":
		return "riscv64-zephyr-elf", nil
	case "arm64", "aarch64":
		return "aarch64-zephyr-elf", nil
	default:
		return "", fmt.Errorf("no zephyr toolchain for arch %q", arch)
	}
}

// InstallBoardToolchain installs the toolchains the target needs if missing;
// one the lockfile doesn't record is an error, never a lockfile change.
func InstallBoardToolchain(appDir, targetName string, log io.Writer) error {
	appDir, err := filepath.Abs(appDir)
	if err != nil {
		return err
	}
	cfg, err := lockfile.ReadConfigFile(filepath.Join(appDir, lockfile.ConfigFilename))
	if err != nil {
		return err
	}
	target, err := cfg.TargetNamed(targetName)
	if err != nil {
		return err
	}
	lock, err := lockfile.ReadFile(filepath.Join(appDir, lockfile.Filename))
	if err != nil {
		return err
	}
	entry, err := lock.DefaultEntry()
	if err != nil {
		return err
	}
	root, err := tools.Root()
	if err != nil {
		return err
	}
	triple, err := host.Current()
	if err != nil {
		return err
	}
	ws, err := WorkspaceDir(root, appDir, entry.Commit)
	if err != nil {
		return err
	}
	needed, err := toolchainsForBoards(boardRoots(appDir, ws, entry.Projects), []string{target.Board})
	if err != nil {
		return err
	}
	locked := map[string]bool{}
	for _, tc := range entry.Toolchains {
		locked[tc] = true
	}
	for _, tc := range needed {
		if !locked[tc] {
			return fmt.Errorf("target %s needs toolchain %s, which %s does not record for zephyr %s; rerun windlock init",
				target.Name, tc, lockfile.Filename, entry.Name)
		}
	}
	hostBin, err := HostToolsBin(lock, root, triple)
	if err != nil {
		return err
	}
	h := hostEnv{root: root, triple: triple, hostBin: hostBin, log: log}
	if _, sdkDir := h.sdkDirs(entry.SDK); len(missingToolchains(sdkDir, needed)) == 0 && SDKInstalled(sdkDir, entry.SDK) {
		return nil
	}
	return installSDK(h, newWorkspace(ws), entry.SDK, needed)
}

// parseAddRevision accepts any git revision. A tag such as v4.4.1 is stored
// under the name without the leading v. Other names are kept as given.
func parseAddRevision(revision string) (name, ref string, err error) {
	ref = strings.TrimSpace(revision)
	if ref == "" {
		return "", "", fmt.Errorf("revision is empty")
	}
	name = ref
	if strings.HasPrefix(ref, "v") && versionLike(ref[1:]) {
		name = ref[1:]
	}
	return name, ref, nil
}

func versionLike(s string) bool {
	if s == "" {
		return false
	}
	digit := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case r == '.':
		default:
			return false
		}
	}
	return digit
}
