//go:build linux && (amd64 || arm64)

package localfs

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

const renameNoReplace = 1

func atomicNoReplaceSupported(parent *os.File, finalName string) bool {
	// Linux validates RENAME_NOREPLACE and selects EEXIST before rejecting the
	// LAST_DOT destination. The first call therefore admits the kernel flag
	// without a lookup or namespace mutation. The self-rename then takes a
	// parent-locked absence snapshot: ENOENT admits the still-absent final name,
	// while a raced object produces EEXIST and is never changed. This admits the
	// kernel primitive and fresh absence only; the real publication remains the
	// destination-filesystem check.
	if err := renameat2NoReplace(parent, finalName, "."); err != syscall.EEXIST {
		return false
	}
	return renameat2NoReplace(parent, finalName, finalName) == syscall.ENOENT
}

func atomicRenameNoReplace(parent *os.File, oldName, newName string) error {
	return renameat2NoReplace(parent, oldName, newName)
}

func renameat2NoReplace(parent *os.File, oldName, newName string) error {
	oldPointer, err := syscall.BytePtrFromString(oldName)
	if err != nil {
		return err
	}
	newPointer, err := syscall.BytePtrFromString(newName)
	if err != nil {
		return err
	}
	for {
		_, _, errno := syscall.Syscall6(
			renameat2Trap,
			parent.Fd(), uintptr(unsafe.Pointer(oldPointer)),
			parent.Fd(), uintptr(unsafe.Pointer(newPointer)),
			renameNoReplace, 0,
		)
		runtime.KeepAlive(parent)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return errno
		}
		return nil
	}
}
