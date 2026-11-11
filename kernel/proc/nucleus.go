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

import (
	"time"
	"unsafe"
)

// Prot is a page's access; read is implied (NUCLEUS_ABI nu_as_map).
type Prot uint8

const (
	ProtWrite Prot = 1
	ProtExec  Prot = 2
	ProtWC    Prot = 4 // device memory, write-combining (UPROT_WC, W21-S04)
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
	Frame(pa uint64) []byte                  // the frame's 4096 bytes, in place
	MemInfo() (totalBytes, freeBytes uint64) // RAM for sysinfo (W14-S03)

	StartThread(as, rip, rsp, fs uint64) (tid uint64, err error)
	Kill(tid uint64) error // returns once the thread is gone
	SetFS(tid, fs uint64) error
	Reply(tid uint64, result int64) error
	Wait(reqs []Request) (int, error) // blocks until at least one request
	// ReplyWait answers tid and waits in one crossing (W25-S03).
	ReplyWait(tid uint64, result int64, reqs []Request) (int, error)

	// W10: the registers of a thread whose request Go holds (fork, execve,
	// signals), a thread started from such an image, a thread moved to
	// another address space, and a stop of a thread computing in ring 3.
	Regs(tid uint64, r *Regs, write bool) error
	StartThreadFrom(as uint64, r *Regs) (uint64, error)
	SetSpace(tid, as uint64) error
	Interrupt(tid uint64) error

	// W23-S02: CPU time, a thread's and every online CPU's.
	ThreadTimes(tid uint64) (user, sys time.Duration, err error)
	CPUTimes() []CPUTime

	// W25-S02: the vDSO's frames (nu_vdso); a zero Vdso when there is none.
	Vdso() (Vdso, error)
}

// Regs is a user thread's register image (NUCLEUS_ABI nu_uthread_regs).
type Regs struct {
	R15, R14, R13, R12, R11, R10, R9, R8, RBP, RDI, RSI, RDX, RCX, RBX, RAX uint64
	RIP, RFLAGS, RSP, FSBase, pad                                           uint64
	FX                                                                      [512]byte // FXSAVE area
}

// FreshRegs is a new program's start: everything zero but the entry, the
// stack, IF, and the x87/SSE defaults a fresh FXSAVE area holds.
func FreshRegs(rip, rsp uint64) Regs {
	r := Regs{RIP: rip, RSP: rsp, RFLAGS: 0x202}
	r.FX[0], r.FX[1] = 0x7F, 0x03   // FCW 0x037F
	r.FX[24], r.FX[25] = 0x80, 0x1F // MXCSR 0x1F80
	return r
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

func (r Request) IsFault() bool  { return r.Nr >= FaultBase && r.Nr != InterruptNr }
func (r Request) Vector() uint64 { return r.Nr &^ FaultBase }

// The nucleus copies requests as 8 little-endian words; Request must match.
var _ [64]byte = [unsafe.Sizeof(Request{})]byte{}

// ...and Regs the nucleus's 672-byte struct uregs.
var _ [672]byte = [unsafe.Sizeof(Regs{})]byte{}

// InterruptNr is a stop asked for with Nucleus.Interrupt (args: rip).
const InterruptNr = FaultBase | 0x100

// MapEntry is one page for BatchMapper.MapBatch: the layout of the
// nucleus's struct uspace_map_entry.
type MapEntry struct{ VA, PA, Prot uint64 }

// MapBatchMax is how many entries one MapBatch call may take.
const MapBatchMax = 4096

// BatchMapper is what a Nucleus may also offer (W18-S06): a range made
// read-only or writable in one call, and many pages mapped in one. Fork
// uses them where they exist; without them it maps page by page.
type BatchMapper interface {
	Protect(as, va, n uint64, prot Prot) error
	MapBatch(as uint64, e []MapEntry) error
}
