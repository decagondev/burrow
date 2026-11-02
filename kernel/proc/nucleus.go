// Package proc is the Go kernel's process POLICY (W9): what a process's
// memory holds, ELF loading, the entry stack, and the Linux system calls it
// makes. The nucleus only supplies MECHANISM - page tables, ring-3 threads,
// the request queue - through the Nucleus interface below: arch/amd64
// implements it with nu_* calls, proctest.Fake in host memory for tests.
//
// User memory is never touched through the process's own page tables: proc
// allocated every frame, so it reads and writes them through the physmap
// (Nucleus.Frame).
package proc

import "unsafe"

// Prot is a page's access; read is implied (NUCLEUS_ABI nu_as_map).
type Prot uint8

const (
	ProtWrite Prot = 1
	ProtExec  Prot = 2
)

const (
	PageSize = 4096
	UserTop  = 1 << 47 // user addresses are below this
)

// Nucleus is the mechanism a process is built on (NUCLEUS_ABI.md 0x1010+).
// Errors are syscall.Errno values from the nucleus.
type Nucleus interface {
	NewSpace() (uint64, error)
	DestroySpace(as uint64) error
	Map(as, va, pa, n uint64, prot Prot) error
	Unmap(as, va, n uint64) error
	PageAlloc() (uint64, error) // one zeroed frame
	PageFree(pa uint64)
	Frame(pa uint64) []byte // the frame's 4096 bytes, in place

	StartThread(as, rip, rsp, fs uint64) (tid uint64, err error)
	Kill(tid uint64) error // returns once the thread is gone
	SetFS(tid, fs uint64) error
	Reply(tid uint64, result int64) error
	Wait(reqs []Request) (int, error) // blocks until at least one request
}

// Request is one user thread's system call or fault, as nu_syscall_wait
// copies it (64 bytes: tid, nr, args[6]).
type Request struct {
	TID  uint64
	Nr   uint64
	Args [6]uint64
}

// FaultBase marks a fault request: Nr = FaultBase | vector; Args = cr2, rip, error.
const FaultBase = 0xFFFF0000

func (r Request) IsFault() bool  { return r.Nr >= FaultBase }
func (r Request) Vector() uint64 { return r.Nr &^ FaultBase }

// The nucleus copies requests as 8 little-endian words; Request must match.
var _ [64]byte = [unsafe.Sizeof(Request{})]byte{}
