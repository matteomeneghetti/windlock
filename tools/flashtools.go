package tools

import (
	"io"
	"path/filepath"
	"runtime"
)

// The flash tools run on a host with the probe on USB; the container never
// installs them. These are the defaults init locks.
const openocdVersion = "0.12.0-7"

func openocdArchive(platform, sum string) Tool {
	file := "xpack-openocd-" + openocdVersion + "-" + platform + ".tar.gz"
	return Tool{
		Name:    "openocd",
		Version: openocdVersion,
		URL:     "https://github.com/xpack-dev-tools/openocd-xpack/releases/download/v" + openocdVersion + "/" + file,
		SHA256:  sum,
	}
}

var dfuUtilArchive = Tool{
	Name:    "dfu-util",
	Version: "0.11",
	URL:     "https://dfu-util.sourceforge.net/releases/dfu-util-0.11.tar.gz",
	SHA256:  "b4b53ba21a82ef7e3d4c47df2952adf5fa494f499b6b0b57c58c5d04ae8ff19e",
}

// buildDFUUtil builds dfu-util into prefix against the lockfile's libusb,
// static, so dfu-util is one binary. Its configure takes USB_CFLAGS and
// USB_LIBS instead of needing pkg-config.
func buildDFUUtil(root string, s Set, archive, sum, prefix string, log io.Writer) error {
	usb, err := ensureSource(root, s, "libusb", log)
	if err != nil {
		return err
	}
	libs := filepath.Join(usb, "lib", "libusb-1.0.a")
	if runtime.GOOS == "darwin" {
		libs += " -framework CoreFoundation -framework IOKit -framework Security -lobjc"
	} else {
		libs += " -lpthread"
	}
	env := append(buildEnv(root, nil), "USB_CFLAGS=-I"+filepath.Join(usb, "include", "libusb-1.0"), "USB_LIBS="+libs)
	return buildFromSourceEnv(root, archive, sum, prefix, env, nil, log)
}
