//go:build darwin && (arm64 || amd64)

// The no-replace rename reaches renameatx_np through syscall·syscall6, which
// src/syscall/syscall_darwin.go exports for syscall and golang.org/x/sys.

#include "textflag.h"

TEXT libc_renameatx_np_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_renameatx_np(SB)

TEXT ·syscallSyscall6(SB),NOSPLIT,$0-80
	JMP	syscall·syscall6(SB)

GLOBL	·libcRenameatxNPTrampolineAddr(SB), RODATA, $8
DATA	·libcRenameatxNPTrampolineAddr(SB)/8, $libc_renameatx_np_trampoline<>(SB)
