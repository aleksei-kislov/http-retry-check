//go:build !aix && !android && !darwin && !dragonfly && !freebsd && !illumos && !ios && !linux && !netbsd && !openbsd && !solaris && !windows

package localfs

import "os"

func openNonblocking(path string) (*os.File, error) {
	return os.Open(path)
}

func nonblockingFlag() int {
	return 0
}
