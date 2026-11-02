// Package uart drives a 16550-compatible serial port.
//
// It is the Go twin of common/serial.c. Same hardware, same register
// sequence - compare them side by side while learning.
package uart

import "burrow/kernel/hal"

const (
	COM1 uint16 = 0x3F8

	regData = 0
	regIER  = 1
	regFCR  = 2
	regLCR  = 3
	regMCR  = 4
	regLSR  = 5

	lsrTHREmpty = 0x20
)

// UART is safe to use from one goroutine at a time; wrap it with a mutex
// (see klog) when several goroutines log.
type UART struct {
	io   hal.PortIO
	base uint16
}

// New does not touch hardware; call Init for that (keeps construction cheap
// and testable).
func New(io hal.PortIO, base uint16) *UART { return &UART{io: io, base: base} }

// Init programs 115200 8N1 with FIFOs enabled and interrupts off.
func (u *UART) Init() {
	u.io.Out8(u.base+regIER, 0x00)
	u.io.Out8(u.base+regLCR, 0x80)  // DLAB on
	u.io.Out8(u.base+regData, 0x01) // divisor 1 -> 115200
	u.io.Out8(u.base+regIER, 0x00)
	u.io.Out8(u.base+regLCR, 0x03) // 8N1
	u.io.Out8(u.base+regFCR, 0xC7)
	u.io.Out8(u.base+regMCR, 0x03)
}

// WriteByte sends one byte, translating '\n' to "\r\n" for terminals.
func (u *UART) WriteByte(c byte) error {
	if c == '\n' {
		u.put('\r')
	}
	u.put(c)
	return nil
}

func (u *UART) put(c byte) {
	for u.io.In8(u.base+regLSR)&lsrTHREmpty == 0 {
	}
	u.io.Out8(u.base+regData, c)
}

// Write makes UART an io.Writer, so fmt.Fprintf / log can target it.
func (u *UART) Write(p []byte) (int, error) {
	for _, c := range p {
		_ = u.WriteByte(c)
	}
	return len(p), nil
}
