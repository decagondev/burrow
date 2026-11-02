//go:build burrow

// Package amd64 holds the real hardware implementations of hal interfaces.
// Built ONLY with -tags burrow (the on-target kernel). Host tests never see
// it, which is exactly why the host tests can run without a VM.
package amd64

// Implemented in portio_amd64.s. Valid only at CPL 0 (or with IOPL set).
func inb(port uint16) uint8
func outb(port uint16, v uint8)

// Ports is the real hal.PortIO.
type Ports struct{}

func (Ports) In8(port uint16) uint8     { return inb(port) }
func (Ports) Out8(port uint16, v uint8) { outb(port, v) }
