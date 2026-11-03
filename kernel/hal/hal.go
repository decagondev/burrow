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

// PortIO16 is 16-bit port I/O: the ATA data port and the Bochs display
// (BGA) registers need it.
type PortIO16 interface {
	In16(port uint16) uint16
	Out16(port uint16, v uint16)
}

// PortIOString is string port I/O (REP INSW / REP OUTSW): many 16-bit words
// to or from one port in a single instruction. An ATA sector is 256 words,
// and an emulator answers one string instruction far faster than 256
// separate ones (W15-S05: a 15 MB Qt program took 11 s to load). Optional:
// a driver falls back to PortIO16 when its ports lack it.
type PortIOString interface {
	InWords(port uint16, dst []byte)  // len(dst)/2 words, little-endian
	OutWords(port uint16, src []byte) // len(src)/2 words, little-endian
}
