package linuxbin

import "os"

// A Linux windlock runs itself in the container, so Linux builds embed nothing.
func selfIsLinux() (string, bool) {
	self, err := os.Executable()
	return self, err == nil
}

func embedded() ([]byte, error) { return nil, ErrMissing }
