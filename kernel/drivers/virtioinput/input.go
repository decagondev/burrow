// Package virtioinput drives a virtio input device (W22-S04, E18.F07):
// QEMU's virtio tablet, PCI 1af4:1052. It reads the device's name and its
// absolute axes from the device configuration, keeps the event queue full of
// 8-byte virtio_input_event buffers and hands each event on (to evdev from
// W22-S05). Never blocks in the interrupt handler; never touches a buffer
// the device owns. Spec: VIRTIO 1.0 §5.8 (input device, from 1.1).
// Docs: docs/04-plan/waves/WAVE-22-sound-tablet.md.
package virtioinput

import (
	"encoding/binary"
	"fmt"
	"strings"

	"burrow/kernel/virtio"
)

const (
	Vendor = 0x1AF4
	Device = 0x1052 // 0x1040 + 18, a modern virtio input device

	cfgSelect   = 0
	cfgSubsel   = 1
	cfgSize     = 2
	cfgUnion    = 8
	cfgIDName   = 0x01
	cfgAbsInfo  = 0x12
	absX, absY  = 0, 1
	eventBytes  = 8
	maxQueue    = 64
	maxNameSize = 128
)

// Event is one virtio_input_event: an evdev type, code and value.
type Event struct {
	Type, Code uint16
	Value      int32
}

func decode(b []byte) Event {
	le := binary.LittleEndian
	return Event{le.Uint16(b), le.Uint16(b[2:]), int32(le.Uint32(b[4:]))}
}

// Axis is an absolute axis's range.
type Axis struct{ Min, Max int32 }

func (a Axis) String() string { return fmt.Sprintf("%d-%d", a.Min, a.Max) }

// query selects one configuration item and returns its bytes.
func query(cfg virtio.Regs, sel, sub uint8) []byte {
	cfg.W8(cfgSelect, sel)
	cfg.W8(cfgSubsel, sub)
	n := int(cfg.R8(cfgSize))
	if n > maxNameSize {
		n = maxNameSize
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = cfg.R8(cfgUnion + uint32(i))
	}
	return b
}

// Name is the device's name ("QEMU Virtio Tablet"; QEMU counts its NUL).
func Name(cfg virtio.Regs) string { return strings.TrimRight(string(query(cfg, cfgIDName, 0)), "\x00") }

// AbsRange is an absolute axis's min and max; ok is false when the device
// has no such axis (a keyboard, a relative mouse).
func AbsRange(cfg virtio.Regs, axis uint8) (Axis, bool) {
	b := query(cfg, cfgAbsInfo, axis)
	if len(b) < 8 {
		return Axis{}, false
	}
	le := binary.LittleEndian
	return Axis{int32(le.Uint32(b)), int32(le.Uint32(b[4:]))}, true
}
