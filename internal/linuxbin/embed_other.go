//go:build !linux

package linuxbin

import (
	"embed"
	"runtime"
)

//go:embed all:bin
var files embed.FS

func selfIsLinux() (string, bool) { return "", false }

func embedded() ([]byte, error) {
	raw, err := files.ReadFile("bin/windlock-linux-" + runtime.GOARCH)
	if err != nil {
		return nil, ErrMissing
	}
	return raw, nil
}
