//go:build !darwin && !linux && !windows

package localfs

import (
	"errors"
	"os"
)

func atomicNoReplaceSupported(_ *os.File, _ string) bool {
	return false
}

func atomicRenameNoReplace(_ *os.File, _, _ string) error {
	return errors.New("atomic no-replace is unavailable")
}
