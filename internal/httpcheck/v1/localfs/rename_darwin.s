//go:build darwin && (arm64 || amd64)

#include "textflag.h"

TEXT libc_renameatx_np_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_renameatx_np(SB)
GLOBL	·libcRenameatxNPTrampolineAddr(SB), RODATA, $8
DATA	·libcRenameatxNPTrampolineAddr(SB)/8, $libc_renameatx_np_trampoline<>(SB)
