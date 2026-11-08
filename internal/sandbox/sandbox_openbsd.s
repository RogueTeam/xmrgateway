//go:build openbsd && (amd64 || arm64 || riscv64)

#include "textflag.h"

TEXT libc_pledge_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_pledge(SB)
GLOBL	·libc_pledge_trampoline_addr(SB), RODATA, $8
DATA	·libc_pledge_trampoline_addr(SB)/8, $libc_pledge_trampoline<>(SB)

TEXT libc_unveil_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_unveil(SB)
GLOBL	·libc_unveil_trampoline_addr(SB), RODATA, $8
DATA	·libc_unveil_trampoline_addr(SB)/8, $libc_unveil_trampoline<>(SB)
