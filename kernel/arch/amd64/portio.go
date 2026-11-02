//go:build burrow

// Package amd64 holds the real hardware implementations of hal interfaces.
// Built ONLY with -tags burrow (the on-target kernel). Host tests never see
// it, which is exactly why the host tests can run without a VM.
package amd64

// Implemented in portio_amd64.s. Valid only at CPL 0 (or with IOPL set).
func inb(port uint16) uint8
func outb(port uint16, v uint8)
func inl(port uint16) uint32
func outl(port uint16, v uint32)
func inw(port uint16) uint16
func outw(port uint16, v uint16)

// Ports is the real hal.PortIO, hal.PortIO16 and hal.PortIO32.
type Ports struct{}

func (Ports) In8(port uint16) uint8       { return inb(port) }
func (Ports) Out8(port uint16, v uint8)   { outb(port, v) }
func (Ports) In32(port uint16) uint32     { return inl(port) }
func (Ports) Out32(port uint16, v uint32) { outl(port, v) }
func (Ports) In16(port uint16) uint16     { return inw(port) }
func (Ports) Out16(port uint16, v uint16) { outw(port, v) }
