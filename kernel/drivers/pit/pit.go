// Package pit drives the 8254 programmable interval timer's channel 0, the
// one wired to ISA IRQ 0. The nucleus keeps time with the LAPIC timer and
// calibrates with channel 2, so channel 0 is free: the Go kernel uses it to
// test the interrupt path end to end (W7-S02).
package pit

import (
	"time"

	"burrow/kernel/hal"
)

const (
	IRQ = 0 // ISA line of channel 0

	InputHz = 1193182 // the PIT's input clock

	portCh0 uint16 = 0x40
	portCmd uint16 = 0x43

	cmdCh0Mode0 = 0x30 // channel 0, lo/hi byte, mode 0 "interrupt on terminal count"
)

// Counts converts d to input-clock ticks, clamped to the 16-bit counter
// (1..65535, about 55 ms at most).
func Counts(d time.Duration) uint16 {
	n := int64(d) * InputHz / int64(time.Second)
	return uint16(min(max(n, 1), 0xFFFF))
}

// OneShot makes channel 0 raise IRQ 0 once, d from now. Writing the mode
// restarts the count, so a pending one-shot is replaced, never doubled.
func OneShot(io hal.PortIO, d time.Duration) {
	c := Counts(d)
	io.Out8(portCmd, cmdCh0Mode0)
	io.Out8(portCh0, uint8(c))
	io.Out8(portCh0, uint8(c>>8))
}
