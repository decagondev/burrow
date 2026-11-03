//go:build burrow

package amd64

import (
	"syscall"
	"unsafe"

	"burrow/kernel/proc"
)

// Nucleus calls for user processes (NUCLEUS_ABI.md 0x1010-0x1024).
const (
	nrASCreate      = 0x1010
	nrASMap         = 0x1011
	nrASUnmap       = 0x1012
	nrPageAlloc     = 0x1014
	nrPageFree      = 0x1015
	nrMemInfo       = 0x1006
	nrASDestroy     = 0x1016
	nrUThreadCreate = 0x1020
	nrSyscallWait   = 0x1021
	nrSyscallReply  = 0x1022
	nrUThreadKill   = 0x1023
	nrUThreadSetFS  = 0x1024
	nrUThreadRegs   = 0x1025
	nrUThreadFrom   = 0x1026
	nrUThreadSpace  = 0x1027
	nrUThreadIntr   = 0x1028
)

// Nucleus is the real proc.Nucleus.
type Nucleus struct{}

var _ proc.Nucleus = Nucleus{}

func raw(nr uintptr, a ...uintptr) (uintptr, error) {
	var args [6]uintptr
	copy(args[:], a)
	r, _, errno := syscall.RawSyscall6(nr, args[0], args[1], args[2], args[3], args[4], args[5])
	if errno != 0 {
		return 0, errno
	}
	return r, nil
}

// blocking is for calls that wait (the runtime moves the P elsewhere).
func blocking(nr uintptr, a ...uintptr) (uintptr, error) {
	var args [3]uintptr
	copy(args[:], a)
	r, _, errno := syscall.Syscall(nr, args[0], args[1], args[2])
	if errno != 0 {
		return 0, errno
	}
	return r, nil
}

func (Nucleus) NewSpace() (uint64, error) {
	r, err := raw(nrASCreate)
	return uint64(r), err
}

func (Nucleus) DestroySpace(as uint64) error { _, err := raw(nrASDestroy, uintptr(as)); return err }

func (Nucleus) Map(as, va, pa, n uint64, prot proc.Prot) error {
	_, err := raw(nrASMap, uintptr(as), uintptr(va), uintptr(pa), uintptr(n), uintptr(prot))
	return err
}

func (Nucleus) Unmap(as, va, n uint64) error {
	_, err := raw(nrASUnmap, uintptr(as), uintptr(va), uintptr(n))
	return err
}

func (Nucleus) PageAlloc() (uint64, error) {
	r, err := raw(nrPageAlloc, 1)
	return uint64(r), err
}

func (Nucleus) PageFree(pa uint64) { raw(nrPageFree, uintptr(pa), 1) }

func (Nucleus) MemInfo() (totalBytes, freeBytes uint64) {
	var m [2]uint64
	raw(nrMemInfo, uintptr(unsafe.Pointer(&m[0])))
	return m[0], m[1]
}

func (Nucleus) Frame(pa uint64) []byte { return PhysBytes(pa, proc.PageSize) }

func (Nucleus) StartThread(as, rip, rsp, fs uint64) (uint64, error) {
	r, err := raw(nrUThreadCreate, uintptr(as), uintptr(rip), uintptr(rsp), uintptr(fs))
	return uint64(r), err
}

func (Nucleus) Kill(tid uint64) error { _, err := blocking(nrUThreadKill, uintptr(tid)); return err }

func (Nucleus) SetFS(tid, fs uint64) error {
	_, err := raw(nrUThreadSetFS, uintptr(tid), uintptr(fs))
	return err
}

func (Nucleus) Reply(tid uint64, result int64) error {
	_, err := raw(nrSyscallReply, uintptr(tid), uintptr(result))
	return err
}

// Wait copies requests straight into reqs (proc.Request is the nucleus's
// 64-byte layout: tid, nr, args[6]).
func (Nucleus) Wait(reqs []proc.Request) (int, error) {
	r, err := blocking(nrSyscallWait, uintptr(unsafe.Pointer(&reqs[0])), uintptr(len(reqs)))
	return int(r), err
}

func (Nucleus) Regs(tid uint64, r *proc.Regs, write bool) error {
	w := uintptr(0)
	if write {
		w = 1
	}
	_, err := raw(nrUThreadRegs, uintptr(tid), uintptr(unsafe.Pointer(r)), w)
	return err
}

func (Nucleus) StartThreadFrom(as uint64, r *proc.Regs) (uint64, error) {
	t, err := raw(nrUThreadFrom, uintptr(as), uintptr(unsafe.Pointer(r)))
	return uint64(t), err
}

func (Nucleus) SetSpace(tid, as uint64) error {
	_, err := raw(nrUThreadSpace, uintptr(tid), uintptr(as))
	return err
}

func (Nucleus) Interrupt(tid uint64) error { _, err := raw(nrUThreadIntr, uintptr(tid)); return err }
