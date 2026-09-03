//go:build linux && (amd64 || arm64)

package localfs

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

const renameNoReplace = 1

func classifyAtomicNoReplaceProbe(err error) atomicNoReplaceProbeStatus {
	if errors.Is(err, syscall.EEXIST) {
		return atomicNoReplaceProbeSupported
	}
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP) {
		return atomicNoReplaceProbeUnsupported
	}
	return atomicNoReplaceProbeFailed
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
		runtime.KeepAlive(oldPointer)
		runtime.KeepAlive(newPointer)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return errno
		}
		return nil
	}
}
