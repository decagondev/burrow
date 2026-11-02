//go:build burrow

// irq_goroutine.go - reference for slice W7-S02 (done: kernel/arch/amd64/irq.go,
// kernel/driver.Serve): "interrupts become goroutine
// wake-ups". Copy into kernel/arch/amd64 when you reach that slice.
//
// The nucleus owns the IDT. When IRQ n fires it masks the line, acknowledges
// the interrupt controller and wakes whichever thread is blocked in
// nu_irq_wait(n). Go code never runs inside an interrupt handler, so the
// GC, stack growth and the scheduler are all safe to use in drivers.
//
//	hardware IRQ1 -> nucleus ISR -> wake thread -> goroutine returns from Wait()
//	-> driver reads port 0x60 -> Ack() unmasks IRQ1
package amd64

import (
	"runtime"
	"syscall"
)

// Nucleus-private syscall numbers live at 0x1000+ so they can never collide
// with Linux numbers (see docs/02-architecture/NUCLEUS_ABI.md).
const (
	sysNuIRQWait = 0x1000
	sysNuIRQAck  = 0x1001
)

// IRQ implements hal.IRQLine for one interrupt vector.
type IRQ struct{ n uintptr }

func NewIRQ(n int) *IRQ { return &IRQ{n: uintptr(n)} }

// Wait blocks until the interrupt fires. syscall.Syscall tells the Go
// scheduler this thread is blocking, so other goroutines keep running.
func (q *IRQ) Wait() error {
	if _, _, errno := syscall.Syscall(sysNuIRQWait, q.n, 0, 0); errno != 0 {
		return errno
	}
	return nil
}

// Ack re-enables (unmasks) the line after the driver has drained the device.
func (q *IRQ) Ack() { syscall.RawSyscall(sysNuIRQAck, q.n, 0, 0) }

// Serve runs handle() once per interrupt on a dedicated OS thread.
func Serve(q *IRQ, handle func()) {
	go func() {
		runtime.LockOSThread() // keep one nucleus thread parked per IRQ line
		for {
			if err := q.Wait(); err != nil {
				return
			}
			handle()
			q.Ack()
		}
	}()
}
