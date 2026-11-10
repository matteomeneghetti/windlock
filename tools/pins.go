package tools

import "fmt"

// The pins below are the defaults windlock init writes to a new lockfile.
// Everything else installs what the lockfile lists; see PINS.md for where
// each checksum comes from and how to update them.

// dtc and gperf are the same source tarballs on every platform.
var dtcArchive = Tool{
	Name:    "dtc",
	Version: "1.8.1",
	URL:     "https://www.kernel.org/pub/software/utils/dtc/dtc-1.8.1.tar.gz",
	SHA256:  "454e5cce748ce957c91e7f45e66da2cff5100ddb52155d7a909c43cbeaf1bcb0",
}

var gperfArchive = Tool{
	Name:    "gperf",
	Version: "3.3",
	URL:     "https://ftp.gnu.org/gnu/gperf/gperf-3.3.tar.gz",
	SHA256:  "fd87e0aba7e43ae054837afd6cd4db03a3f2693deb3619085e6ed9d8d9604ad8",
}

// linuxX8664Build is what the container installs to build on x86_64.
var linuxX8664Build = []Tool{
	{
		Name:    "cmake",
		Version: "3.31.12",
		URL:     "https://github.com/Kitware/CMake/releases/download/v3.31.12/cmake-3.31.12-linux-x86_64.tar.gz",
		SHA256:  "0dc2e9a6860f06bf10bd8fadc03e35d9eeb4df46e33763a7e480e987758f385c",
	},
	{
		Name:    "ninja",
		Version: "1.13.2",
		URL:     "https://github.com/ninja-build/ninja/releases/download/v1.13.2/ninja-linux.zip",
		SHA256:  "5749cbc4e668273514150a80e387a957f933c6ed3f5f11e03fb30955e2bbead6",
	},
	{
		Name:    "python",
		Version: "3.12.15",
		URL:     "https://github.com/astral-sh/python-build-standalone/releases/download/20261021/cpython-3.12.15+20261021-x86_64-unknown-linux-gnu-install_only.tar.gz",
		SHA256:  "5ff9f0602ce964a529ed1e8555d3abab6341926d724402ba9a15e9e877191124",
	},
	{
		Name:    "uv",
		Version: "0.13.6",
		URL:     "https://github.com/astral-sh/uv/releases/download/0.13.6/uv-x86_64-unknown-linux-gnu.tar.gz",
		SHA256:  "f29c3e94ffabd7e6f3ba521dba169a9b7ec12100bf3257087f0f24f6123f3645",
	},
	dtcArchive,
	gperfArchive,
}

// linuxAarch64Build is what the container installs to build on arm64.
var linuxAarch64Build = []Tool{
	{
		Name:    "cmake",
		Version: "3.31.12",
		URL:     "https://github.com/Kitware/CMake/releases/download/v3.31.12/cmake-3.31.12-linux-aarch64.tar.gz",
		SHA256:  "83f8fd91d2038a56556e1400390fcfe42f79602940c494f6c6f1cdae7f9e7f40",
	},
	{
		Name:    "ninja",
		Version: "1.13.2",
		URL:     "https://github.com/ninja-build/ninja/releases/download/v1.13.2/ninja-linux-aarch64.zip",
		SHA256:  "fd2cacc8050a7f12a16a2e48f9e06fca5c14fc4c2bee2babb67b58be17a607fc",
	},
	{
		Name:    "python",
		Version: "3.12.15",
		URL:     "https://github.com/astral-sh/python-build-standalone/releases/download/20261021/cpython-3.12.15+20261021-aarch64-unknown-linux-gnu-install_only.tar.gz",
		SHA256:  "f53cca8fcd20805bddaa8bff1ef722a6918f8225d37561bd36ad901d13d580af",
	},
	{
		Name:    "uv",
		Version: "0.13.6",
		URL:     "https://github.com/astral-sh/uv/releases/download/0.13.6/uv-aarch64-unknown-linux-gnu.tar.gz",
		SHA256:  "229e194255c7958d86d56e8790d969a6f74736592133f0dfd0076f754c642c1d",
	},
	dtcArchive,
	gperfArchive,
}

// A host with the probe needs Python and uv for its west, and the flash
// tools. The Mac only ever does this; builds run in the container.
var macosAarch64Flash = []Tool{
	{
		Name:    "python",
		Version: "3.12.15",
		URL:     "https://github.com/astral-sh/python-build-standalone/releases/download/20261021/cpython-3.12.15+20261021-aarch64-apple-darwin-install_only.tar.gz",
		SHA256:  "a18cc33862b53bd54a148c710def5375c1b47470cccedf817a19c2d211764584",
	},
	{
		Name:    "uv",
		Version: "0.13.6",
		URL:     "https://github.com/astral-sh/uv/releases/download/0.13.6/uv-aarch64-apple-darwin.tar.gz",
		SHA256:  "60fbd5c8a1ed681845eea28d9d622fcde03c51e91a43465b64a1e17db1ed3472",
	},
	openocdArchive("darwin-arm64", "667342c086984f3e5a55b4e0d5f711add13fb04de040fca493303000e6c19327"),
	dfuUtilArchive,
}

var macosX8664Flash = []Tool{
	{
		Name:    "python",
		Version: "3.12.15",
		URL:     "https://github.com/astral-sh/python-build-standalone/releases/download/20261021/cpython-3.12.15+20261021-x86_64-apple-darwin-install_only.tar.gz",
		SHA256:  "869e0b30959beccc616ce14907439f4d0b3170c5830a2362dade07f07b0ed3b8",
	},
	{
		Name:    "uv",
		Version: "0.13.6",
		URL:     "https://github.com/astral-sh/uv/releases/download/0.13.6/uv-x86_64-apple-darwin.tar.gz",
		SHA256:  "63ba17fdda55ad1a230d4b37fc6017ffa10c23688c83818bd3bfdf914c652a02",
	},
	openocdArchive("darwin-x64", "668ad25350103a4357e11629ec833eae5982e973889ce25bad0c2963e37fa8bf"),
	dfuUtilArchive,
}

// defaultSources are the source tarballs recipes build with: m4, flex and
// bison for dtc, and libusb, linked static into dfu-util.
var defaultSources = []Tool{
	{
		Name:    "m4",
		Version: "1.4.21",
		URL:     "https://ftp.gnu.org/gnu/m4/m4-1.4.21.tar.gz",
		SHA256:  "38ae59f7a30bf9c108193cc5c25fbb06014f21e230c7ede2eff614f7b7c37ed8",
	},
	{
		Name:    "flex",
		Version: "2.6.4",
		URL:     "https://github.com/westes/flex/releases/download/v2.6.4/flex-2.6.4.tar.gz",
		SHA256:  "e87aae032bf07c26f85ac0ed3250998c37621d95f8bd748b31f15b33c45ee995",
	},
	{
		Name:    "bison",
		Version: "3.8.2",
		URL:     "https://ftp.gnu.org/gnu/bison/bison-3.8.2.tar.gz",
		SHA256:  "06c9e13bdf7eb24d4ceb6b59205a4f67c2c7e7213119644430fe82fbd14a0abb",
	},
	{
		Name:    "libusb",
		Version: "1.0.30",
		URL:     "https://github.com/libusb/libusb/releases/download/v1.0.30/libusb-1.0.30.tar.bz2",
		SHA256:  "fea36f34f9156400209595e300840767ab1a385ede1dc7ee893015aea9c6dbaf",
	},
}

// ContainerImage is Zephyr's ci-base v0.29.4, pinned by its multi-arch index
// digest (linux/amd64 and linux/arm64). It supplies git, make and cc; every
// pinned tool is installed inside it from the lockfile.
const ContainerImage = "ghcr.io/zephyrproject-rtos/ci-base@sha256:0a4e08683a5582748696dd28d1633566be04218174811eb532e170a13e14959b"

// BinNames are the executables an installed tool set's bin directory can hold.
var BinNames = []string{"cmake", "ninja", "python3", "dtc", "gperf", "uv", "openocd", "dfu-util"}

// DefaultBuildTools are the tools init locks for the container to build
// with on triple. Only Linux triples build.
func DefaultBuildTools(triple string) ([]Tool, error) {
	switch triple {
	case "linux-x86_64":
		return clone(linuxX8664Build), nil
	case "linux-aarch64":
		return clone(linuxAarch64Build), nil
	}
	return nil, fmt.Errorf("no build tools for %s: builds run in the Linux container", triple)
}

// DefaultFlashTools are the tools init locks for a host with the probe.
func DefaultFlashTools(triple string) ([]Tool, error) {
	switch triple {
	case "linux-x86_64":
		return linuxFlash(linuxX8664Build, "linux-x64", "94b3790983beaf8ed57e646c0620dd66d705fddae03d290823a6ed3b439468d6"), nil
	case "linux-aarch64":
		return linuxFlash(linuxAarch64Build, "linux-arm64", "db73a3ab91c556ecec2405a7e02d404b11139df6aba1031cad94a7e6766d06cc"), nil
	case "macos-aarch64":
		return clone(macosAarch64Flash), nil
	case "macos-x86_64":
		return clone(macosX8664Flash), nil
	}
	return nil, fmt.Errorf("no flash tools for %s", triple)
}

// DefaultSources are the source tarballs init locks for the recipes.
func DefaultSources() []Tool { return clone(defaultSources) }

// linuxFlash is a Linux host's probe side: the Python and uv the container
// builds with, and the flash tools.
func linuxFlash(build []Tool, openocdPlatform, openocdSHA256 string) []Tool {
	var out []Tool
	for _, t := range build {
		if t.Name == "python" || t.Name == "uv" {
			out = append(out, t)
		}
	}
	return append(out, openocdArchive(openocdPlatform, openocdSHA256), dfuUtilArchive)
}

func clone(ts []Tool) []Tool { return append([]Tool(nil), ts...) }
