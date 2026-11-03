//go:build burrow

#include "textflag.h"

// func cpuid(leaf, sub uint32) (a, b, c, d uint32)
TEXT ·cpuid(SB), NOSPLIT, $0-24
	MOVL leaf+0(FP), AX
	MOVL sub+4(FP), CX
	CPUID
	MOVL AX, a+8(FP)
	MOVL BX, b+12(FP)
	MOVL CX, c+16(FP)
	MOVL DX, d+20(FP)
	RET

// func rdrand() (v uint64, ok bool)
// CF=1 when the value is valid.
TEXT ·rdrand(SB), NOSPLIT, $0-9
	RDRANDQ AX
	SETCS ok+8(FP)
	MOVQ AX, v+0(FP)
	RET

// func rdtsc() uint64
TEXT ·rdtsc(SB), NOSPLIT, $0-8
	RDTSC
	SHLQ $32, DX
	ORQ DX, AX
	MOVQ AX, ret+0(FP)
	RET
