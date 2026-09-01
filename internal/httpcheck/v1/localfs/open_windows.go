//go:build windows

package localfs

import "os"

func openNonblocking(path string) (*os.File, error) {
	return os.Open(path)
}

func nonblockingFlag() int {
	return 0
}
