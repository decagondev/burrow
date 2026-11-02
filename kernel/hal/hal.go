// Package hal defines the Hardware Abstraction Layer: small interfaces that
// describe WHAT the kernel needs from hardware, never HOW it is done.
//
// SOLID in one file:
//   - Interface Segregation: each interface is 1-3 methods. A driver asks for
//     exactly what it uses (the UART needs PortIO, nothing else).
//   - Dependency Inversion: drivers depend on these interfaces; the real
//     implementations live in arch/amd64 (build tag "burrow") and the test
//     doubles live in haltest. Neither side imports the other.
package hal

// PortIO is x86 port-mapped I/O (IN/OUT instructions).
type PortIO interface {
	In8(port uint16) uint8
	Out8(port uint16, v uint8)
}

// Clock is a monotonic time source in nanoseconds since boot.
type Clock interface {
	Nanotime() int64
}

// IRQLine lets a driver goroutine sleep until its interrupt fires.
// The nucleus turns hardware interrupts into wake-ups (see ADR-0005).
type IRQLine interface {
	Wait() error // blocks the calling goroutine (its OS thread), not the kernel
	Ack()
}

// PortIO32 is 32-bit port I/O, which PCI configuration mechanism #1 needs
// (0xCF8/0xCFC). Separate from PortIO so 8-bit drivers do not have to fake it.
type PortIO32 interface {
	In32(port uint16) uint32
	Out32(port uint16, v uint32)
}
