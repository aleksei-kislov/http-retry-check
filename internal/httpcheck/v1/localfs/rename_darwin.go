//go:build darwin

package localfs

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

const renameExclusive = 0x4

//go:uintptrescapes
func syscallSyscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno)

var libcRenameatxNPTrampolineAddr uintptr

//go:cgo_import_dynamic libc_renameatx_np renameatx_np "/usr/lib/libSystem.B.dylib"

func classifyAtomicNoReplaceProbe(err error) atomicNoReplaceProbeStatus {
	if err == nil || errors.Is(err, syscall.EEXIST) {
		return atomicNoReplaceProbeSupported
	}
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP) {
		return atomicNoReplaceProbeUnsupported
	}
	return atomicNoReplaceProbeFailed
}

func atomicRenameNoReplace(parent *os.File, oldName, newName string) error {
	return renameatxNP(parent, oldName, newName)
}

func renameatxNP(parent *os.File, oldName, newName string) error {
	oldPointer, err := syscall.BytePtrFromString(oldName)
	if err != nil {
		return err
	}
	newPointer, err := syscall.BytePtrFromString(newName)
	if err != nil {
		return err
	}
	for {
		_, _, errno := syscallSyscall6(
			libcRenameatxNPTrampolineAddr,
			parent.Fd(), uintptr(unsafe.Pointer(oldPointer)),
			parent.Fd(), uintptr(unsafe.Pointer(newPointer)),
			renameExclusive, 0,
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
