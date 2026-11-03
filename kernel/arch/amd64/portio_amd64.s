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

// func inl(port uint16) uint32
TEXT ·inl(SB), NOSPLIT, $0-12
	MOVW port+0(FP), DX
	INL
	MOVL AX, ret+8(FP)
	RET

// func outl(port uint16, v uint32)
TEXT ·outl(SB), NOSPLIT, $0-8
	MOVW port+0(FP), DX
	MOVL v+4(FP), AX
	OUTL
	RET

// func inw(port uint16) uint16
TEXT ·inw(SB), NOSPLIT, $0-10
	MOVW port+0(FP), DX
	XORL AX, AX
	INW
	MOVW AX, ret+8(FP)
	RET

// func outw(port uint16, v uint16)
TEXT ·outw(SB), NOSPLIT, $0-4
	MOVW port+0(FP), DX
	MOVW v+2(FP), AX
	OUTW
	RET

// func insw(port uint16, dst *byte, words int)
// Clobbers DX, DI, CX; the direction flag is cleared first.
TEXT ·insw(SB), NOSPLIT, $0-24
	MOVW port+0(FP), DX
	MOVQ dst+8(FP), DI
	MOVQ words+16(FP), CX
	CLD
	REP; INSW
	RET

// func outsw(port uint16, src *byte, words int)
// Clobbers DX, SI, CX; the direction flag is cleared first.
TEXT ·outsw(SB), NOSPLIT, $0-24
	MOVW port+0(FP), DX
	MOVQ src+8(FP), SI
	MOVQ words+16(FP), CX
	CLD
	REP; OUTSW
	RET
