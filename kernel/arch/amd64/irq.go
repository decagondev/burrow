//go:build burrow

package amd64

import (
	"syscall"
	"unsafe"
)

// Nucleus-private calls for interrupt lines (NUCLEUS_ABI.md, nucleus/irq.c).
const (
	nrIRQWait     = 0x1000
	nrIRQAck      = 0x1001
	nrIRQAllocMSI = 0x102E // W27-S01
	nrIRQRoute    = 0x102F
)

// AllocMSI takes a new MSI line from the nucleus: its number (16 up) and
// the message a device must write to raise it (W27-S01).
func AllocMSI() (line int, addr uint64, data uint32, err error) {
	var out [2]uint64
	r, err := raw(nrIRQAllocMSI, uintptr(unsafe.Pointer(&out[0])))
	if err != nil {
		return 0, 0, 0, err
	}
	return int(r), out[0], uint32(out[1]), nil
}

// IRQRoute is where line goes, as the nucleus encodes it (driver.ParseRoute).
func IRQRoute(line int) (uint64, error) {
	r, err := raw(nrIRQRoute, uintptr(line))
	return uint64(r), err
}

// IRQ is the real hal.IRQLine for one interrupt line: an ISA line (0-15,
// not 2) or an MSI line (W27-S01). The nucleus owns the interrupt: its ISR
// masks a level line, sends EOI and wakes the thread
// blocked in Wait. Go code never runs in an interrupt handler (ADR-0005), so
// a driver may allocate, block and take locks like any goroutine.
//
//	IRQ n -> nucleus ISR -> wake thread -> Wait returns -> driver drains the
//	device -> Ack unmasks the line
type IRQ struct{ line uintptr }

func NewIRQ(line int) *IRQ { return &IRQ{line: uintptr(line)} }

// Wait blocks until the line fires (at once if it fired since the last
// Wait). syscall.Syscall, not RawSyscall: the runtime sees a blocking call
// and hands this thread's P to another thread, so other goroutines run.
func (q *IRQ) Wait() error {
	if _, _, errno := syscall.Syscall(nrIRQWait, q.line, 0, 0); errno != 0 {
		return errno
	}
	return nil
}

// Ack unmasks the line once the device is drained.
func (q *IRQ) Ack() { syscall.RawSyscall(nrIRQAck, q.line, 0, 0) }
