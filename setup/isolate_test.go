package setup

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestChildEnvSetsToolchainVariant(t *testing.T) {
	t.Setenv("ZEPHYR_TOOLCHAIN_VARIANT", "gnuarmemb")
	t.Setenv("ZEPHYR_SDK_INSTALL_DIR", "/elsewhere")
	root := t.TempDir()
	env := childEnv(filepath.Join(root, "bin"), "", root, "")
	joined := "\n" + strings.Join(env, "\n") + "\n"
	if !strings.Contains(joined, "\nZEPHYR_TOOLCHAIN_VARIANT=zephyr\n") || strings.Contains(joined, "gnuarmemb") {
		t.Fatal("install env does not pin ZEPHYR_TOOLCHAIN_VARIANT=zephyr")
	}
	if strings.Contains(joined, "ZEPHYR_SDK_INSTALL_DIR=") {
		t.Fatal("install env leaks ZEPHYR_SDK_INSTALL_DIR")
	}
	for _, want := range []string{"PIP_CACHE_DIR=" + filepath.Join(root, "cache", "pip"), "CCACHE_DIR=" + filepath.Join(root, "cache", "ccache"), "PYTHONNOUSERSITE=1", "XDG_CACHE_HOME=" + filepath.Join(root, "cache", "xdg")} {
		if !strings.Contains(joined, "\n"+want+"\n") {
			t.Fatalf("install env lacks %s", want)
		}
	}
}
