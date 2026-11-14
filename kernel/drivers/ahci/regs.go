// Package ahci drives SATA disks behind an AHCI host bus adapter (W27-S02):
// QEMU's ICH9 controller on q35 (and `-device ahci` on pc), as on most PCs
// of the last fifteen years. Each port with an ATA disk becomes a
// block.Writer named sda, sdb, ... : one command at a time per port, its
// data through DMA pages (never the Go heap, PIT-44), READ/WRITE DMA EXT
// and FLUSH CACHE EXT, completion by interrupt (MSI where the controller
// has it). The firmware may leave ports running with its own lists, so
// every port is stopped and given ours before use. ATAPI and port
// multipliers are not driven. AHCI 1.3.1; ATA8-ACS. Docs: WAVE-27-real-pc.md.
package ahci

import "encoding/binary"

// Regs is the HBA's memory-mapped registers (BAR 5, "ABAR").
type Regs interface {
	R32(off uint32) uint32
	W32(off uint32, v uint32)
}

// HBA registers (AHCI 1.3.1 §3.1).
const (
	hbaCAP = 0x00
	hbaGHC = 0x04
	hbaIS  = 0x08
	hbaPI  = 0x0C

	capS64A = 1 << 31
	ghcAE   = 1 << 31
	ghcIE   = 1 << 1

	portBase  = 0x100
	portBytes = 0x80
	maxPorts  = 32
	ABARBytes = portBase + maxPorts*portBytes
)

// Port registers (§3.3), offsets from the port's base.
const (
	pxCLB  = 0x00
	pxCLBU = 0x04
	pxFB   = 0x08
	pxFBU  = 0x0C
	pxIS   = 0x10
	pxIE   = 0x14
	pxCMD  = 0x18
	pxTFD  = 0x20
	pxSIG  = 0x24
	pxSSTS = 0x28
	pxSERR = 0x30
	pxCI   = 0x38

	cmdST  = 1 << 0
	cmdFRE = 1 << 4
	cmdFR  = 1 << 14
	cmdCR  = 1 << 15

	tfdERR = 1 << 0
	tfdDRQ = 1 << 3
	tfdBSY = 1 << 7

	isTFES  = 1 << 30
	ieAll   = 0x7DC000FF // every completion and error cause AHCI defines
	sstsDET = 0xF
	detUp   = 3 // a device, and communication established
	sigATA  = 0x00000101
)

// The port's DMA page: command list (32 headers), received FISes, then
// slot 0's command table: the command FIS, then the PRDT.
const (
	pageBytes   = 4096
	clbOff      = 0
	fbOff       = 1024
	ctOff       = 2048
	prdtOff     = 0x80
	prdtBytes   = 16
	pagesMax    = 16 // data pages per command: 64 KiB, 128 sectors
	sectorBytes = 512
	cflDwords   = 5 // a register FIS is 20 bytes
	hdrWrite    = 1 << 6
)

// ATA commands and the register FIS (ATA8-ACS; SATA 3.0 §10.3.4).
const (
	fisRegH2D    = 0x27
	fisCommand   = 0x80
	devLBA       = 1 << 6
	ataIdentify  = 0xEC
	ataReadExt   = 0x25
	ataWriteExt  = 0x35
	ataFlushExt  = 0xEA
	identifyLBA4 = 83 // word: bit 10 = 48-bit addresses
)

var le = binary.LittleEndian
