//go:build burrow

package amd64

import "syscall"

// Nucleus-private calls (docs/02-architecture/NUCLEUS_ABI.md). They use the
// Linux syscall convention, so the stock syscall.RawSyscall reaches them: no
// assembly, no runtime changes.
const (
	nrPhysmapBase = 0x1003
	nrDebugExit   = 0x1004
	nrReset       = 0x1030
)

// PhysmapBase is the virtual address of physical 0 (all RAM is mapped there).
func PhysmapBase() uintptr {
	r, _, _ := syscall.RawSyscall(nrPhysmapBase, 0, 0, 0)
	return r
}

// DebugExit ends a QEMU test run with status (code<<1)|1 (0x10 = pass). On
// real hardware, and in `make run`, there is no debug-exit device: it returns.
func DebugExit(code uint8) {
	syscall.RawSyscall(nrDebugExit, uintptr(code), 0, 0)
}

// Reset restarts the machine (W28-S04), trying each way from the one
// named on: 0 the FADT's reset register, 1 port 0xCF9, 2 the keyboard
// controller, 3 a triple fault. It does not return.
func Reset(from int) {
	syscall.RawSyscall(nrReset, uintptr(from), 0, 0)
}
