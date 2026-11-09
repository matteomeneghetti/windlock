package setup

import (
	"path/filepath"

	"github.com/matteomeneghetti/windlock/internal/envvar"
)

// Isolate points every child's caches (pip, ccache, XDG, so also Zephyr's
// compiler cache, sysbuild images included) at <root>/cache.
func Isolate(env []string, root string) []string {
	cache := filepath.Join(root, "cache")
	env = envvar.Set(env, "PYTHONNOUSERSITE", "1")
	env = envvar.Set(env, "PIP_DISABLE_PIP_VERSION_CHECK", "1")
	env = envvar.Set(env, "PIP_CACHE_DIR", filepath.Join(cache, "pip"))
	env = envvar.Set(env, "CCACHE_DIR", filepath.Join(cache, "ccache"))
	env = envvar.Set(env, "XDG_CACHE_HOME", filepath.Join(cache, "xdg"))
	return env
}
