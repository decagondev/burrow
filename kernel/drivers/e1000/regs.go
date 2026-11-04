// Package e1000 drives the Intel 82540EM Ethernet card (PCI 8086:100E),
// QEMU's default NIC (W17-S01): reset, the MAC address from the EEPROM, one
// receive and one transmit descriptor ring in frames the nucleus hands out
// (physical memory that never moves: DMA must not see Go's heap, PIT-44),
// and receive and transmit-done interrupts served as wake-ups (ADR-0005).
// The card registers as netdev "eth0". Registers sit behind Regs and DMA
// pages behind a function, so host tests drive a fake card.
// Reference: Intel's PCI/PCI-X Family of Gigabit Ethernet Controllers
// Software Developer's Manual (8254x), chapters 3, 13 and 14.
// Docs: docs/04-plan/waves/WAVE-17-networking.md.
package e1000

import (
	"sync/atomic"
	"unsafe"
)

const (
	Vendor = 0x8086
	Device = 0x100E // 82540EM

	MMIOBytes = 0x20000 // BAR 0: 128 KiB of registers

	regCTRL   = 0x0000
	regSTATUS = 0x0008
	regEERD   = 0x0014
	regICR    = 0x00C0 // reading clears it
	regIMS    = 0x00D0
	regIMC    = 0x00D8
	regRCTL   = 0x0100
	regTCTL   = 0x0400
	regTIPG   = 0x0410
	regRDBAL  = 0x2800
	regRDBAH  = 0x2804
	regRDLEN  = 0x2808
	regRDH    = 0x2810
	regRDT    = 0x2818
	regTDBAL  = 0x3800
	regTDBAH  = 0x3804
	regTDLEN  = 0x3808
	regTDH    = 0x3810
	regTDT    = 0x3818
	regMTA    = 0x5200 // 128 words of multicast hash
	regRAL0   = 0x5400
	regRAH0   = 0x5404

	mtaWords = 128

	ctrlASDE   = 1 << 5  // auto speed detection
	ctrlSLU    = 1 << 6  // set link up
	ctrlLRST   = 1 << 3  // link reset
	ctrlRST    = 1 << 26 // device reset (self-clearing)
	ctrlVME    = 1 << 30
	ctrlPHYRST = 1 << 31

	statusLU = 1 << 1 // link up

	eerdStart = 1 << 0
	eerdDone  = 1 << 4

	rahAV = 1 << 31 // the receive address is valid

	rctlEN    = 1 << 1
	rctlBAM   = 1 << 15 // accept broadcast
	rctlSECRC = 1 << 26 // strip the CRC; BSIZE 00 = 2048-byte buffers

	tctlEN   = 1 << 1
	tctlPSP  = 1 << 3     // pad short packets
	tctlCT   = 0x0F << 4  // collision threshold
	tctlCOLD = 0x40 << 12 // collision distance, full duplex
	tipgIEEE = 0x0060200A // IPGT 10, IPGR1 8, IPGR2 6

	intTXDW   = 1 << 0 // a transmit descriptor written back
	intLSC    = 1 << 2 // link status change
	intRXDMT0 = 1 << 4 // receive descriptors running low
	intRXO    = 1 << 6 // receive overrun
	intRXT0   = 1 << 7 // a frame received
	intWanted = intTXDW | intLSC | intRXDMT0 | intRXO | intRXT0
)

// Regs is the card's register file.
type Regs interface {
	Read(off uint32) uint32
	Write(off, v uint32)
}

// MMIO is Regs over the mapped BAR. Every access is one 32-bit load or
// store (atomics compile to exactly that on amd64; the store is also a
// fence, so descriptors written before a tail update reach memory first).
type MMIO []byte

func (m MMIO) Read(off uint32) uint32 {
	return atomic.LoadUint32((*uint32)(unsafe.Pointer(&m[off])))
}

func (m MMIO) Write(off, v uint32) {
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&m[off])), v)
}
