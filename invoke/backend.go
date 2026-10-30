package invoke

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// containerMarker records, in a build directory, that this windlock's
// container configured it.
const containerMarker = ".windlock-container"

// removeStaleBuild deletes buildDir when it holds a CMake cache the container
// did not write. Its paths may not exist here, and west's own pristine runs a
// script from the cached Zephyr path, so -p always cannot clear it.
func removeStaleBuild(buildDir string, log io.Writer) error {
	if _, err := os.Stat(filepath.Join(buildDir, "CMakeCache.txt")); err != nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(buildDir, containerMarker)); err == nil {
		return nil
	}
	fmt.Fprintf(log, "removing %s: it was not configured by this windlock's container\n", buildDir)
	return os.RemoveAll(buildDir)
}

// recordContainer marks buildDir as configured by the container. It runs
// after the build, since a pristine build deletes the directory first.
func recordContainer(buildDir string) error {
	if _, err := os.Stat(buildDir); err != nil {
		return nil
	}
	return os.WriteFile(filepath.Join(buildDir, containerMarker), nil, 0o644)
}
