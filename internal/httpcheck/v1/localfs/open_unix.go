//go:build aix || android || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd || solaris

package localfs

import (
	"os"
	"syscall"
)

const safeReadFlags = syscall.O_NONBLOCK | syscall.O_NOFOLLOW | syscall.O_NOCTTY

func openNonblocking(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|safeReadFlags, 0)
}

func nonblockingFlag() int {
	return safeReadFlags
}
