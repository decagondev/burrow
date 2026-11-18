// Package hda drives an Intel High Definition Audio controller (W28-S03):
// the controller reset, the CORB and RIRB rings that carry codec verbs and
// their responses, the codecs found in STATESTS, each codec's widget graph
// walked from an output pin back to a DAC (codec.go), and one output
// stream whose buffer descriptor list loops over a ring of DMA pages
// (stream.go), served as sound.Output for /dev/dsp like the AC'97's.
// Registers sit behind Regs and DMA pages behind a function, so host tests
// drive a fake controller and codec. Never plays from Go's heap (PIT-44),
// never assumes fixed node ids (PIT-108). Reference: Intel's High
// Definition Audio Specification 1.0a, chapters 3 (controller registers)
// and 7 (codec verbs and parameters). Docs:
// docs/04-plan/waves/WAVE-28-more-devices.md.
package hda

import (
	"encoding/binary"
	"sync/atomic"
	"unsafe"
)

// Controller registers (spec §3.3).
const (
	regGCAP      = 0x00 // 16: OSS 15:12, ISS 11:8, BSS 7:3, 64OK bit 0
	regGCTL      = 0x08 // 32: CRST bit 0
	regSTATESTS  = 0x0E // 16: a bit per codec that answered the reset
	regINTCTL    = 0x20 // 32: GIE 31, CIE 30, SIE bits per stream
	regINTSTS    = 0x24 // 32: GIS 31, CIS 30, SIS bits per stream
	regCORBLBASE = 0x40
	regCORBUBASE = 0x44
	regCORBWP    = 0x48 // 16
	regCORBRP    = 0x4A // 16: RST bit 15
	regCORBCTL   = 0x4C // 8: RUN bit 1
	regCORBSIZE  = 0x4E // 8: size 1:0, capability 7:4
	regRIRBLBASE = 0x50
	regRIRBUBASE = 0x54
	regRIRBWP    = 0x58 // 16: RST bit 15
	regRINTCNT   = 0x5A // 16
	regRIRBCTL   = 0x5C // 8: RUN bit 1
	regRIRBSTS   = 0x5D // 8: write 1 to clear
	regRIRBSIZE  = 0x5E // 8

	gctlCRST    = 1 << 0
	intGIE      = 1 << 31
	corbRPRST   = 1 << 15
	rirbWPRST   = 1 << 15
	ringRun     = 1 << 1
	rirbRINTCTL = 1 << 0 // RIRBCTL: flag responses in RIRBSTS
	ringSize256 = 2      // CORBSIZE/RIRBSIZE 1:0 = 256 entries
	ringCap256  = 1 << 6 // CORBSIZE/RIRBSIZE: 256 entries supported
	ringSize16  = 1
	ringCap16   = 1 << 5

	streamBase  = 0x80 // stream descriptor 0
	streamBytes = 0x20 // input streams, then output, then bidirectional
)

// Stream descriptor registers (spec §3.3.35-3.3.44), from its base.
const (
	sdCTL  = 0x00 // 24 bits: SRST 0, RUN 1, IOCE 2, FEIE 3, DEIE 4, stream number 23:20
	sdSTS  = 0x03 // 8: BCIS 2, FIFOE 3, DESE 4 (write 1 to clear)
	sdLPIB = 0x04
	sdCBL  = 0x08
	sdLVI  = 0x0C // 16
	sdFMT  = 0x12 // 16
	sdBDPL = 0x18
	sdBDPU = 0x1C

	ctlSRST    = 1 << 0
	ctlRUN     = 1 << 1
	ctlIOCE    = 1 << 2
	ctlStrmSh  = 20
	stsBCIS    = 1 << 2
	stsErrors  = 1<<3 | 1<<4
	bdlEntry   = 16
	bdlIOC     = 1 << 0
	streamTag  = 1 // the output stream's number on the link (1-15)
	fourGiBPA  = 1 << 32
	pageBytes  = 4096
	corbOffset = 0    // in the ring page
	rirbOffset = 2048 // 256 responses of 8 bytes; 128-byte aligned
)

// Regs is the controller's register file, in the widths it has.
type Regs interface {
	R8(off uint32) uint8
	R16(off uint32) uint16
	R32(off uint32) uint32
	W8(off uint32, v uint8)
	W16(off uint32, v uint16)
	W32(off uint32, v uint32)
}

// MMIO is Regs over the mapped BAR. Every access is one load or store of
// the register's own width at its own offset: a controller decodes them so
// (QEMU's returns the register at the address, whatever the width), and
// RIRBSTS and SDnSTS are write-1-to-clear, so a wider store would clear
// their neighbours. The narrow ones are calls the compiler cannot fold
// away or reorder.
type MMIO []byte

func (m MMIO) R8(off uint32) uint8   { return load8((*uint8)(unsafe.Pointer(&m[off]))) }
func (m MMIO) R16(off uint32) uint16 { return load16((*uint16)(unsafe.Pointer(&m[off]))) }
func (m MMIO) R32(off uint32) uint32 { return atomic.LoadUint32((*uint32)(unsafe.Pointer(&m[off]))) }

func (m MMIO) W8(off uint32, v uint8)   { store8((*uint8)(unsafe.Pointer(&m[off])), v) }
func (m MMIO) W16(off uint32, v uint16) { store16((*uint16)(unsafe.Pointer(&m[off])), v) }
func (m MMIO) W32(off uint32, v uint32) {
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&m[off])), v)
}

//go:noinline
func load8(p *uint8) uint8 { return *p }

//go:noinline
func load16(p *uint16) uint16 { return *p }

//go:noinline
func store8(p *uint8, v uint8) { *p = v }

//go:noinline
func store16(p *uint16, v uint16) { *p = v }

// DMAPage hands out one zeroed page the controller can reach: its physical
// address and its bytes (driver.Env.DMAPage).
type DMAPage func() (pa uint64, mem []byte, err error)

var le = binary.LittleEndian
