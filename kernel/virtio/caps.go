// Package virtio is the virtio 1.0 PCI transport (W22-S04, E18.F07): where a
// device keeps its configuration structures, the status handshake and
// feature negotiation, split virtqueues and their notifications. Pure: it
// reads and writes through Regs and plain byte slices (DMA pages), so a fake
// device in Go plays the other side in tests; drivers (virtioinput now,
// block and network later) map the real registers. Modern devices only: the
// legacy I/O interface of transitional devices is never used (PIT-74).
// Reference: Virtual I/O Device (VIRTIO) Version 1.0, §2 and §4.1.
// Docs: docs/04-plan/waves/WAVE-22-sound-tablet.md.
package virtio

import (
	"errors"
	"fmt"
)

// PCI configuration space.
const (
	pciStatus       = 0x04 // the upper half of this dword
	pciStatusCaps   = 1 << (16 + 4)
	pciCapPtr       = 0x34
	capVendor       = 0x09 // a virtio_pci_cap is a vendor-specific capability
	maxCaps         = 48   // a list longer than config space allows is broken
	capCfgType      = 3
	capBAR          = 4
	capOffset       = 8
	capLength       = 12
	capNotifyFactor = 16

	cfgCommon = 1
	cfgNotify = 2
	cfgISR    = 3
	cfgDevice = 4
)

// Region is one configuration structure: length bytes at offset in a BAR.
type Region struct {
	BAR            uint8
	Offset, Length uint32
}

// Caps are a device's configuration structures and its notify stride.
type Caps struct {
	Common, Notify, ISR, Device Region
	NotifyMultiplier            uint32
}

// FindCaps walks the capability list through read (a configuration-space
// dword at an aligned offset) and keeps the first structure of each type.
func FindCaps(read func(off uint8) uint32) (Caps, error) {
	byteAt := func(off uint8) uint8 { return uint8(read(off&^3) >> (8 * (off & 3))) }
	var c Caps
	if read(pciStatus)&pciStatusCaps == 0 {
		return c, errors.New("virtio: no PCI capability list")
	}
	found := map[uint8]bool{}
	ptr := byteAt(pciCapPtr) &^ 3
	for i := 0; ptr != 0 && i < maxCaps; i++ {
		if byteAt(ptr) == capVendor {
			t := byteAt(ptr + capCfgType)
			r := Region{BAR: byteAt(ptr + capBAR), Offset: read(ptr + capOffset), Length: read(ptr + capLength)}
			if !found[t] {
				found[t] = true
				switch t {
				case cfgCommon:
					c.Common = r
				case cfgNotify:
					c.Notify = r
					c.NotifyMultiplier = read(ptr + capNotifyFactor)
				case cfgISR:
					c.ISR = r
				case cfgDevice:
					c.Device = r
				}
			}
		}
		ptr = byteAt(ptr+1) &^ 3
	}
	for _, t := range []uint8{cfgCommon, cfgNotify, cfgISR, cfgDevice} {
		if !found[t] {
			return c, fmt.Errorf("virtio: no capability of type %d (a legacy-only device?)", t)
		}
	}
	return c, nil
}
