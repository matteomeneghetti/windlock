//go:build !unix

package rootlock

import "os"

// Windows runs only the container launcher; it takes no lock yet.
func tryLock(*os.File) (bool, error) { return true, nil }

func waitLock(*os.File) error { return nil }

func unlock(*os.File) error { return nil }
