package tools

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The flash tools run on the host, where the probe is on USB. Only
// InstallFlash installs them; the container never needs them.
const openocdVersion = "0.12.0-7"

func openocdArchive(platform, sum string) Archive {
	file := "xpack-openocd-" + openocdVersion + "-" + platform + ".tar.gz"
	return Archive{
		Name:    "openocd",
		Version: openocdVersion,
		URL:     "https://github.com/xpack-dev-tools/openocd-xpack/releases/download/v" + openocdVersion + "/" + file,
		SHA256:  sum,
		File:    file,
		Kind:    kindOpenOCD,
	}
}

var dfuUtilArchive = Archive{
	Name:    "dfu-util",
	Version: "0.11",
	URL:     "https://dfu-util.sourceforge.net/releases/dfu-util-0.11.tar.gz",
	SHA256:  "b4b53ba21a82ef7e3d4c47df2952adf5fa494f499b6b0b57c58c5d04ae8ff19e",
	File:    "dfu-util-0.11.tar.gz",
	Kind:    kindDFUUtil,
	SrcDir:  "dfu-util-0.11",
}

// libusbArchive is built static into cache/build-tools, so dfu-util is one
// binary. It is not part of the host-tools id.
var libusbArchive = Archive{
	Name:    "libusb",
	Version: "1.0.30",
	URL:     "https://github.com/libusb/libusb/releases/download/v1.0.30/libusb-1.0.30.tar.bz2",
	SHA256:  "fea36f34f9156400209595e300840767ab1a385ede1dc7ee893015aea9c6dbaf",
	File:    "libusb-1.0.30.tar.bz2",
	Kind:    kindAutotools,
	SrcDir:  "libusb-1.0.30",
}

// flashOnly reports whether a is a flash tool, which only InstallFlash installs.
func (a Archive) flashOnly() bool { return a.Kind == kindOpenOCD || a.Kind == kindDFUUtil }

// forFlash reports whether InstallFlash installs a: the flash tools, and
// the Python and uv the host's west environment runs on.
func (a Archive) forFlash() bool { return a.flashOnly() || a.Name == "python" || a.Name == "uv" }

// ensureLibusb builds the pinned libusb, static, into cache/build-tools.
func ensureLibusb(root string, log io.Writer) (prefix string, err error) {
	prefix = filepath.Join(root, "cache", "build-tools")
	marker := filepath.Join(prefix, ".installed", libusbArchive.Name)
	lib := filepath.Join(prefix, "lib", "libusb-1.0.a")
	if installed(marker, lib, libusbArchive.SHA256) {
		return prefix, nil
	}
	arc, err := installArchive(root, libusbArchive, log)
	if err != nil {
		return "", err
	}
	args := []string{"--disable-shared", "--enable-static"}
	if runtime.GOOS == "linux" {
		args = append(args, "--disable-udev")
	}
	if err := buildAutotoolsWith(root, arc, libusbArchive.SrcDir, prefix, commandEnv(nil), args, log); err != nil {
		return "", fmt.Errorf("libusb: %w", err)
	}
	if _, err := os.Stat(lib); err != nil {
		return "", fmt.Errorf("libusb: %s was not installed", lib)
	}
	return prefix, copyToMode(strings.NewReader(libusbArchive.SHA256+"\n"), marker, 0o644)
}

// buildDFUUtil builds dfu-util into prefix against the static libusb; its
// configure takes USB_CFLAGS and USB_LIBS instead of needing pkg-config.
func buildDFUUtil(root, archive, srcDir, prefix string, log io.Writer) error {
	usb, err := ensureLibusb(root, log)
	if err != nil {
		return err
	}
	libs := filepath.Join(usb, "lib", "libusb-1.0.a")
	if runtime.GOOS == "darwin" {
		libs += " -framework CoreFoundation -framework IOKit -framework Security -lobjc"
	} else {
		libs += " -lpthread"
	}
	env := commandEnv(nil)
	env = append(env, "USB_CFLAGS=-I"+filepath.Join(usb, "include", "libusb-1.0"), "USB_LIBS="+libs)
	return buildAutotoolsWith(root, archive, srcDir, prefix, env, nil, log)
}

// InstallFlash installs what the host needs to flash and debug builds made
// in the container: Python and uv for west, OpenOCD and dfu-util.
func InstallFlash(log io.Writer) (bin string, err error) {
	return install(log, Archive.forFlash)
}
