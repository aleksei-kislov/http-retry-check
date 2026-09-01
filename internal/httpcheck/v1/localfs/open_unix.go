//go:build aix || android || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd || solaris

package localfs

import (
	"os"
	"syscall"
)

func openNonblocking(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}

func nonblockingFlag() int {
	return syscall.O_NONBLOCK
}
