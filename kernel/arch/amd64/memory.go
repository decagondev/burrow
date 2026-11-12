//go:build burrow

package amd64

import (
	"syscall"
	"unsafe"
)

const (
	nrBootInfo = 0x1002
	nrMapMMIO  = 0x1005

	bootInfoBytes = 128 // sizeof(struct boot_info), include/burrow/boot_info.h
)

// BootInfo returns a copy of boot_info v4 (parse it with package bootinfo).
func BootInfo() []byte {
	buf := make([]byte, bootInfoBytes)
	n, _, _ := syscall.RawSyscall(nrBootInfo, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	return buf[:n]
}

// PhysBytes is physical memory [pa, pa+n) seen through the physmap, where
// the nucleus maps all RAM (boot modules, the module table). No copy.
func PhysBytes(pa, n uint64) []byte {
	return unsafe.Slice((*byte)(addrToPointer(PhysmapBase()+uintptr(pa))), n)
}

// MapMMIO maps device memory [pa, pa+n) (a PCI BAR), uncached, and returns it.
func MapMMIO(pa, n uint64) ([]byte, error) { return mapMMIO(pa, n, 0) }

// MapMMIOWC maps device memory write-combining (a framebuffer, W21-S04):
// uncached where the CPU has no PAT; EEXIST if it is mapped already with
// another cache type.
func MapMMIOWC(pa, n uint64) ([]byte, error) { return mapMMIO(pa, n, mmioWC) }

const mmioWC = 1 // nu_mmio_map's NU_MMIO_WC

func mapMMIO(pa, n uint64, flags uintptr) ([]byte, error) {
	va, _, errno := syscall.RawSyscall(nrMapMMIO, uintptr(pa), uintptr(n), flags)
	if errno != 0 {
		return nil, errno
	}
	return unsafe.Slice((*byte)(addrToPointer(va)), n), nil
}

// addrToPointer turns an address the nucleus handed us into a pointer. The
// memory is not Go heap, so the garbage collector ignores pointers into it;
// vet's unsafeptr check cannot know that, hence the indirection.
func addrToPointer(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}
