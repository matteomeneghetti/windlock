package tools

import (
	"fmt"

	"github.com/matteomeneghetti/windlock/host"
)

// Kind selects how an archive is installed.
type Kind int

const (
	kindCMake Kind = iota
	kindNinja
	kindPython
	kindGperf
	kindDTC
	kindAutotools
	kindUV
	kindOpenOCD
	kindDFUUtil
)

// Archive is one verified upstream file; SHA256 is the checksum of the bytes
// at URL. See PINS.md for where each checksum comes from.
type Archive struct {
	Name    string
	Version string
	URL     string
	SHA256  string
	File    string
	Kind    Kind
	// SrcDir is the top directory inside a source tarball.
	SrcDir string
}

// Pins are the same versions on every platform; PINS.md records each
// checksum's source, the version choices, and how to update them.
var dtcArchive = Archive{
	Name:    "dtc",
	Version: "1.8.1",
	URL:     "https://www.kernel.org/pub/software/utils/dtc/dtc-1.8.1.tar.gz",
	SHA256:  "454e5cce748ce957c91e7f45e66da2cff5100ddb52155d7a909c43cbeaf1bcb0",
	File:    "dtc-1.8.1.tar.gz",
	Kind:    kindDTC,
	SrcDir:  "dtc-1.8.1",
}

var gperfArchive = Archive{
	Name:    "gperf",
	Version: "3.3",
	URL:     "https://ftp.gnu.org/gnu/gperf/gperf-3.3.tar.gz",
	SHA256:  "fd87e0aba7e43ae054837afd6cd4db03a3f2693deb3619085e6ed9d8d9604ad8",
	File:    "gperf-3.3.tar.gz",
	Kind:    kindGperf,
	SrcDir:  "gperf-3.3",
}

var linuxHostPins = []Archive{
	{
		Name:    "cmake",
		Version: "3.31.12",
		URL:     "https://github.com/Kitware/CMake/releases/download/v3.31.12/cmake-3.31.12-linux-x86_64.tar.gz",
		SHA256:  "0dc2e9a6860f06bf10bd8fadc03e35d9eeb4df46e33763a7e480e987758f385c",
		File:    "cmake-3.31.12-linux-x86_64.tar.gz",
		Kind:    kindCMake,
	},
	{
		Name:    "ninja",
		Version: "1.13.2",
		URL:     "https://github.com/ninja-build/ninja/releases/download/v1.13.2/ninja-linux.zip",
		SHA256:  "5749cbc4e668273514150a80e387a957f933c6ed3f5f11e03fb30955e2bbead6",
		File:    "ninja-linux.zip",
		Kind:    kindNinja,
	},
	{
		Name:    "python",
		Version: "3.12.15",
		URL:     "https://github.com/astral-sh/python-build-standalone/releases/download/20261021/cpython-3.12.15+20261021-x86_64-unknown-linux-gnu-install_only.tar.gz",
		SHA256:  "5ff9f0602ce964a529ed1e8555d3abab6341926d724402ba9a15e9e877191124",
		File:    "cpython-3.12.15+20261021-x86_64-unknown-linux-gnu-install_only.tar.gz",
		Kind:    kindPython,
	},
	{
		Name:    "uv",
		Version: "0.13.6",
		URL:     "https://github.com/astral-sh/uv/releases/download/0.13.6/uv-x86_64-unknown-linux-gnu.tar.gz",
		SHA256:  "f29c3e94ffabd7e6f3ba521dba169a9b7ec12100bf3257087f0f24f6123f3645",
		File:    "uv-x86_64-unknown-linux-gnu.tar.gz",
		Kind:    kindUV,
	},
	dtcArchive,
	gperfArchive,
	openocdArchive("linux-x64", "94b3790983beaf8ed57e646c0620dd66d705fddae03d290823a6ed3b439468d6"),
	dfuUtilArchive,
}

var linuxAarch64Pins = []Archive{
	{
		Name:    "cmake",
		Version: "3.31.12",
		URL:     "https://github.com/Kitware/CMake/releases/download/v3.31.12/cmake-3.31.12-linux-aarch64.tar.gz",
		SHA256:  "83f8fd91d2038a56556e1400390fcfe42f79602940c494f6c6f1cdae7f9e7f40",
		File:    "cmake-3.31.12-linux-aarch64.tar.gz",
		Kind:    kindCMake,
	},
	{
		Name:    "ninja",
		Version: "1.13.2",
		URL:     "https://github.com/ninja-build/ninja/releases/download/v1.13.2/ninja-linux-aarch64.zip",
		SHA256:  "fd2cacc8050a7f12a16a2e48f9e06fca5c14fc4c2bee2babb67b58be17a607fc",
		File:    "ninja-linux-aarch64.zip",
		Kind:    kindNinja,
	},
	{
		Name:    "python",
		Version: "3.12.15",
		URL:     "https://github.com/astral-sh/python-build-standalone/releases/download/20261021/cpython-3.12.15+20261021-aarch64-unknown-linux-gnu-install_only.tar.gz",
		SHA256:  "f53cca8fcd20805bddaa8bff1ef722a6918f8225d37561bd36ad901d13d580af",
		File:    "cpython-3.12.15+20261021-aarch64-unknown-linux-gnu-install_only.tar.gz",
		Kind:    kindPython,
	},
	{
		Name:    "uv",
		Version: "0.13.6",
		URL:     "https://github.com/astral-sh/uv/releases/download/0.13.6/uv-aarch64-unknown-linux-gnu.tar.gz",
		SHA256:  "229e194255c7958d86d56e8790d969a6f74736592133f0dfd0076f754c642c1d",
		File:    "uv-aarch64-unknown-linux-gnu.tar.gz",
		Kind:    kindUV,
	},
	dtcArchive,
	gperfArchive,
	openocdArchive("linux-arm64", "db73a3ab91c556ecec2405a7e02d404b11139df6aba1031cad94a7e6766d06cc"),
	dfuUtilArchive,
}

// The Mac runs only the probe side: Python and uv for its west, and the
// flash tools. Everything that builds runs in the container, on Linux pins.
var macosAarch64Pins = []Archive{
	{
		Name:    "python",
		Version: "3.12.15",
		URL:     "https://github.com/astral-sh/python-build-standalone/releases/download/20261021/cpython-3.12.15+20261021-aarch64-apple-darwin-install_only.tar.gz",
		SHA256:  "a18cc33862b53bd54a148c710def5375c1b47470cccedf817a19c2d211764584",
		File:    "cpython-3.12.15+20261021-aarch64-apple-darwin-install_only.tar.gz",
		Kind:    kindPython,
	},
	{
		Name:    "uv",
		Version: "0.13.6",
		URL:     "https://github.com/astral-sh/uv/releases/download/0.13.6/uv-aarch64-apple-darwin.tar.gz",
		SHA256:  "60fbd5c8a1ed681845eea28d9d622fcde03c51e91a43465b64a1e17db1ed3472",
		File:    "uv-aarch64-apple-darwin.tar.gz",
		Kind:    kindUV,
	},
	openocdArchive("darwin-arm64", "667342c086984f3e5a55b4e0d5f711add13fb04de040fca493303000e6c19327"),
	dfuUtilArchive,
}

var macosX8664Pins = []Archive{
	{
		Name:    "python",
		Version: "3.12.15",
		URL:     "https://github.com/astral-sh/python-build-standalone/releases/download/20261021/cpython-3.12.15+20261021-x86_64-apple-darwin-install_only.tar.gz",
		SHA256:  "869e0b30959beccc616ce14907439f4d0b3170c5830a2362dade07f07b0ed3b8",
		File:    "cpython-3.12.15+20261021-x86_64-apple-darwin-install_only.tar.gz",
		Kind:    kindPython,
	},
	{
		Name:    "uv",
		Version: "0.13.6",
		URL:     "https://github.com/astral-sh/uv/releases/download/0.13.6/uv-x86_64-apple-darwin.tar.gz",
		SHA256:  "63ba17fdda55ad1a230d4b37fc6017ffa10c23688c83818bd3bfdf914c652a02",
		File:    "uv-x86_64-apple-darwin.tar.gz",
		Kind:    kindUV,
	},
	openocdArchive("darwin-x64", "668ad25350103a4357e11629ec833eae5982e973889ce25bad0c2963e37fa8bf"),
	dfuUtilArchive,
}

var dtcBuildDeps = []Archive{
	{
		Name:    "m4",
		Version: "1.4.21",
		URL:     "https://ftp.gnu.org/gnu/m4/m4-1.4.21.tar.gz",
		SHA256:  "38ae59f7a30bf9c108193cc5c25fbb06014f21e230c7ede2eff614f7b7c37ed8",
		File:    "m4-1.4.21.tar.gz",
		Kind:    kindAutotools,
		SrcDir:  "m4-1.4.21",
	},
	{
		Name:    "flex",
		Version: "2.6.4",
		URL:     "https://github.com/westes/flex/releases/download/v2.6.4/flex-2.6.4.tar.gz",
		SHA256:  "e87aae032bf07c26f85ac0ed3250998c37621d95f8bd748b31f15b33c45ee995",
		File:    "flex-2.6.4.tar.gz",
		Kind:    kindAutotools,
		SrcDir:  "flex-2.6.4",
	},
	{
		Name:    "bison",
		Version: "3.8.2",
		URL:     "https://ftp.gnu.org/gnu/bison/bison-3.8.2.tar.gz",
		SHA256:  "06c9e13bdf7eb24d4ceb6b59205a4f67c2c7e7213119644430fe82fbd14a0abb",
		File:    "bison-3.8.2.tar.gz",
		Kind:    kindAutotools,
		SrcDir:  "bison-3.8.2",
	},
}

// ContainerImage is Zephyr's ci-base v0.29.4, pinned by its multi-arch index
// digest (linux/amd64 and linux/arm64). It supplies git, make and cc; every
// pinned tool is installed inside it from the lockfile.
const ContainerImage = "ghcr.io/zephyrproject-rtos/ci-base@sha256:0a4e08683a5582748696dd28d1633566be04218174811eb532e170a13e14959b"

// BinNames are the executables env expects in the host-tools bin directory.
var BinNames = []string{"cmake", "ninja", "python3", "dtc", "gperf", "uv", "openocd", "dfu-util"}

// HostPins returns the host-tool archives for triple. Linux pins are what
// the container installs; macOS pins are the Mac's probe side only.
func HostPins(triple string) ([]Archive, error) {
	var pins []Archive
	switch triple {
	case "linux-x86_64":
		pins = linuxHostPins
	case "linux-aarch64":
		pins = linuxAarch64Pins
	case "macos-aarch64":
		pins = macosAarch64Pins
	case "macos-x86_64":
		pins = macosX8664Pins
	default:
		return nil, fmt.Errorf("no host-tool pin for %s", triple)
	}
	out := make([]Archive, len(pins))
	copy(out, pins)
	return out, nil
}

// PinnedTriples are the host triples HostPins has archives for.
func PinnedTriples() []string {
	var out []string
	for _, triple := range host.Triples {
		if _, err := HostPins(triple); err == nil {
			out = append(out, triple)
		}
	}
	return out
}

func versionsOf(pins []Archive) []Version {
	out := make([]Version, len(pins))
	for i, p := range pins {
		out[i] = Version{Name: p.Name, Version: p.Version}
	}
	return out
}
