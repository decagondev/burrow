// msi.go - capabilities and message-signalled interrupts (W27-S01): the
// capability list a function's status register announces, MSI (one
// message, address and data in configuration space) and MSI-X (a table of
// messages in a BAR; entry 0 is all Burrow uses). Pure: configuration space
// through ConfigWriter, the MSI-X table through a 32-bit writer, so tests
// feed fakes. Never enables MSI-X with the function mask left on, and never
// writes a capability it did not find. PCI Local Bus 3.0 §6.7, §6.8;
// ADR-0014.
package pci

import "errors"

const (
	offCapPtr       = 0x34
	statusCapList   = 1 << 20 // in the command/status dword: the status's bit 4
	cmdINTxDisable  = 1 << 10
	maxCapHops      = 48
	capMSI          = 0x05
	capMSIX         = 0x11
	msiEnable       = 1 << 16 // message control is the upper half of the cap's first dword
	msiMultiMask    = 7 << 20 // multiple message enable: 0 = one message
	msi64Bit        = 1 << 23
	msixEnable      = 1 << 31
	msixFunctionMsk = 1 << 30
	msixSizeMask    = 0x7FF << 16
	msixEntryBytes  = 16
)

// Cap is one capability: its ID and where it sits in configuration space.
type Cap struct{ ID, Off uint8 }

// Capabilities walks d's capability list (none if its status says there is
// no list); a loop or a pointer into the header ends the walk.
func Capabilities(cs ConfigSpace, d Device) []Cap {
	if cs.Read32(d.Bus, d.Dev, d.Fn, offCommand)&statusCapList == 0 {
		return nil
	}
	var caps []Cap
	off := uint8(cs.Read32(d.Bus, d.Dev, d.Fn, offCapPtr)) &^ 3
	for hops := 0; off >= 0x40 && hops < maxCapHops; hops++ {
		w := cs.Read32(d.Bus, d.Dev, d.Fn, off)
		caps = append(caps, Cap{ID: uint8(w), Off: off})
		off = uint8(w>>8) &^ 3
	}
	return caps
}

// FindCap is where capability id starts, if d has it.
func FindCap(caps []Cap, id uint8) (uint8, bool) {
	for _, c := range caps {
		if c.ID == id {
			return c.Off, true
		}
	}
	return 0, false
}

// EnableMSI makes d send one message, data to addr, for its interrupt, and
// stops its INTx pin.
func EnableMSI(cw ConfigWriter, d Device, off uint8, addr uint64, data uint32) {
	ctl := cw.Read32(d.Bus, d.Dev, d.Fn, off)
	cw.Write32(d.Bus, d.Dev, d.Fn, off, ctl&^(msiEnable|msiMultiMask))
	cw.Write32(d.Bus, d.Dev, d.Fn, off+4, uint32(addr))
	dataOff := off + 8
	if ctl&msi64Bit != 0 {
		cw.Write32(d.Bus, d.Dev, d.Fn, off+8, uint32(addr>>32))
		dataOff = off + 12
	}
	cw.Write32(d.Bus, d.Dev, d.Fn, dataOff, data&0xFFFF)
	disableINTx(cw, d)
	cw.Write32(d.Bus, d.Dev, d.Fn, off, (ctl&^msiMultiMask)|msiEnable)
}

// MSIXTable is where d's MSI-X table is: a BAR, an offset in it, the entries.
func MSIXTable(cs ConfigSpace, d Device, off uint8) (bar int, offset uint32, entries int) {
	ctl := cs.Read32(d.Bus, d.Dev, d.Fn, off)
	t := cs.Read32(d.Bus, d.Dev, d.Fn, off+4)
	return int(t & 7), t &^ 7, int((ctl&msixSizeMask)>>16) + 1
}

// EnableMSIX writes entry 0 of d's table (through w32, offsets from the
// table's start), unmasked, and turns MSI-X on with the function unmasked.
func EnableMSIX(cw ConfigWriter, d Device, off uint8, w32 func(off, v uint32), addr uint64, data uint32) error {
	if w32 == nil {
		return errors.New("pci: no MSI-X table")
	}
	ctl := cw.Read32(d.Bus, d.Dev, d.Fn, off)
	cw.Write32(d.Bus, d.Dev, d.Fn, off, (ctl|msixFunctionMsk)&^msixEnable)
	w32(0, uint32(addr))
	w32(4, uint32(addr>>32))
	w32(8, data)
	w32(12, 0) // vector control: unmasked
	disableINTx(cw, d)
	cw.Write32(d.Bus, d.Dev, d.Fn, off, (ctl|msixEnable)&^msixFunctionMsk)
	return nil
}

func disableINTx(cw ConfigWriter, d Device) {
	cmd := cw.Read32(d.Bus, d.Dev, d.Fn, offCommand) & 0xFFFF
	cw.Write32(d.Bus, d.Dev, d.Fn, offCommand, cmd|cmdINTxDisable)
}
