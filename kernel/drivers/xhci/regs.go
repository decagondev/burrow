// Package xhci drives USB 3 host controllers (W27-S03): QEMU's qemu-xhci
// and the USB controller of every PC since about 2012. The controller is
// taken from the firmware (USB legacy support), reset, given its device
// context array, a command ring and one event ring on interrupter 0 (MSI-X
// where it has it), and run. Each connected root-hub port is reset, its
// device given a slot and an address, its descriptors read, and the device
// handed to package usb as a usb.Device: control transfers on endpoint 0,
// pipes on the others. Only what HID and storage need: no isochronous
// endpoints, no streams, no external hubs (WAVE-27's scope). Every ring,
// context and buffer the controller touches is a DMA page (PIT-44), never
// Go heap. eXtensible Host Controller Interface 1.2 (the "spec" below).
// Docs: docs/04-plan/waves/WAVE-27-real-pc.md (S03), ADR-0014.
package xhci

import "encoding/binary"

// Regs is the controller's registers (BAR 0): 32-bit reads and writes;
// 64-bit registers are written as two halves, low first (spec §5.1).
type Regs interface {
	R32(off uint32) uint32
	W32(off uint32, v uint32)
}

// DMAPage hands out a zeroed 4 KiB page: its physical address and bytes.
type DMAPage func() (pa uint64, mem []byte, err error)

// Capability registers (spec §5.3).
const (
	capLength  = 0x00 // byte 0: where the operational registers start
	hcsParams1 = 0x04 // MaxSlots 7:0, MaxIntrs 18:8, MaxPorts 31:24
	hcsParams2 = 0x08 // scratchpad buffers: hi 25:21, lo 31:27
	hccParams1 = 0x10 // AC64 bit 0, CSZ bit 2, xECP 31:16 (in dwords)
	dbOff      = 0x14
	rtsOff     = 0x18

	hccCSZ = 1 << 2 // contexts are 64 bytes, not 32
)

// Operational registers (spec §5.4), from capLength.
const (
	opUSBCMD   = 0x00
	opUSBSTS   = 0x04
	opPageSize = 0x08
	opCRCR     = 0x18
	opDCBAAP   = 0x30
	opConfig   = 0x38
	opPorts    = 0x400
	portStride = 0x10

	cmdRun   = 1 << 0
	cmdReset = 1 << 1
	cmdINTE  = 1 << 2

	stsHalted = 1 << 0
	stsHSE    = 1 << 2 // host system error
	stsEINT   = 1 << 3
	stsCNR    = 1 << 11 // controller not ready
	stsHCE    = 1 << 12 // host controller error

	crcrRCS = 1 << 0
)

// PORTSC (spec §5.4.8). PED and the change bits are write-1-to-clear: a
// write keeps them 0 except the one being cleared (portWrite).
const (
	portCCS        = 1 << 0
	portPED        = 1 << 1
	portPR         = 1 << 4
	portPP         = 1 << 9
	portSpeedShift = 10
	portSpeedMask  = 0xF
	portCSC        = 1 << 17
	portPRC        = 1 << 21
	portChanges    = 0x7F << 17 // CSC PEC WRC OCC PRC PLC CEC
)

// Interrupter 0 (spec §5.5.2), from rtsOff.
const (
	irIMAN   = 0x20
	irIMOD   = 0x24
	irERSTSZ = 0x28
	irERSTBA = 0x30
	irERDP   = 0x38

	imanIP  = 1 << 0
	imanIE  = 1 << 1
	erdpEHB = 1 << 3
)

// Extended capabilities (spec §7).
const (
	xcapLegacy      = 1
	legacyBIOSOwned = 1 << 16
	legacyOSOwned   = 1 << 24
	legacyCtlSts    = 4          // USBLEGCTLSTS, after USBLEGSUP
	legacySMIOff    = 0xE0000000 // its write-1-to-clear SMI events; enables 0
)

const pageBytes = 4096

var le = binary.LittleEndian

func (h *Controller) op(off uint32) uint32 { return h.r.R32(h.opBase + off) }
func (h *Controller) setOp(off, v uint32)  { h.r.W32(h.opBase+off, v) }
func (h *Controller) setOp64(off uint32, v uint64) {
	h.r.W32(h.opBase+off, uint32(v))
	h.r.W32(h.opBase+off+4, uint32(v>>32))
}
func (h *Controller) setRT64(off uint32, v uint64) {
	h.r.W32(h.rtBase+off, uint32(v))
	h.r.W32(h.rtBase+off+4, uint32(v>>32))
}
func (h *Controller) ring(slot, target uint32) { h.r.W32(h.dbBase+4*slot, target) }

func (h *Controller) portsc(port int) uint32 {
	return h.op(opPorts + portStride*uint32(port-1))
}

// portWrite writes PORTSC with set added, clearing only the change bits in
// clear (never PED, which a 1 would disable).
func (h *Controller) portWrite(port int, set, clear uint32) {
	v := h.portsc(port) &^ (portPED | portChanges)
	h.setOp(opPorts+portStride*uint32(port-1), v|set|clear)
}
