//go:build darwin

package localfs

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

const renameExclusive = 0x4

const (
	darwinAttributeBitmapCount          = 5
	darwinAttributeVolumeCapabilities   = 0x00020000
	darwinAttributeVolumeInformation    = 0x80000000
	darwinVolumeCapabilityInterfaces    = 1
	darwinVolumeCapabilityRenameExclude = 0x00080000
)

type darwinAttributeList struct {
	BitmapCount uint16
	Reserved    uint16
	Common      uint32
	Volume      uint32
	Directory   uint32
	File        uint32
	Fork        uint32
}

type darwinVolumeCapabilities struct {
	Length       uint32
	Capabilities [4]uint32
	Valid        [4]uint32
}

func syscallSyscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno)

//go:linkname syscallSyscall6 syscall.syscall6

var libcRenameatxNPTrampolineAddr uintptr

//go:cgo_import_dynamic libc_renameatx_np renameatx_np "/usr/lib/libSystem.B.dylib"

func atomicNoReplaceSupported(parent *os.File, _ string) bool {
	attributes := darwinAttributeList{
		BitmapCount: darwinAttributeBitmapCount,
		Volume:      darwinAttributeVolumeInformation | darwinAttributeVolumeCapabilities,
	}
	capabilities := darwinVolumeCapabilities{}
	_, _, errno := syscall.Syscall6(
		syscall.SYS_FGETATTRLIST,
		parent.Fd(),
		uintptr(unsafe.Pointer(&attributes)),
		uintptr(unsafe.Pointer(&capabilities)),
		unsafe.Sizeof(capabilities),
		0,
		0,
	)
	runtime.KeepAlive(parent)
	runtime.KeepAlive(&attributes)
	runtime.KeepAlive(&capabilities)
	if errno != 0 || capabilities.Length != uint32(unsafe.Sizeof(capabilities)) {
		return false
	}
	valid := capabilities.Valid[darwinVolumeCapabilityInterfaces]
	supported := capabilities.Capabilities[darwinVolumeCapabilityInterfaces]
	return valid&darwinVolumeCapabilityRenameExclude != 0 &&
		supported&darwinVolumeCapabilityRenameExclude != 0
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
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return errno
		}
		return nil
	}
}
