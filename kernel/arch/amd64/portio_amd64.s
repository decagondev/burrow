//go:build burrow

#include "textflag.h"

// func inb(port uint16) uint8
TEXT ·inb(SB), NOSPLIT, $0-9
	MOVW port+0(FP), DX
	XORL AX, AX
	INB
	MOVB AX, ret+8(FP)
	RET

// func outb(port uint16, v uint8)
TEXT ·outb(SB), NOSPLIT, $0-3
	MOVW port+0(FP), DX
	MOVB v+2(FP), AX
	OUTB
	RET
