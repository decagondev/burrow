// Package nvme drives NVMe disks (W27-S02): QEMU's `-device nvme` and the
// SSDs of most PCs since 2016. A controller is reset, given an admin queue
// pair (polled, used only while setting up), identified, and given one I/O
// queue pair whose completions interrupt (MSI-X where it has it).
// Namespace 1 becomes a block.Writer named nvme0n1, nvme1n1, ... : a few
// commands in flight, each with its own DMA data pages and PRP list (never
// the Go heap, PIT-44). Never rings a doorbell for an entry it has not
// written, and never reuses a command identifier the controller has not
// completed. NVM Express Base 2.0, NVM Command Set 1.0.
// Docs: docs/04-plan/waves/WAVE-27-real-pc.md (S02).
package nvme

import "encoding/binary"

// Regs is the controller's registers (BAR 0): 32-bit reads and writes; the
// 64-bit registers are read and written as two halves, low first.
type Regs interface {
	R32(off uint32) uint32
	W32(off uint32, v uint32)
}

// Controller registers (NVMe Base 2.0 §3.1).
const (
	regCAP   = 0x00
	regCC    = 0x14
	regCSTS  = 0x1C
	regAQA   = 0x24
	regASQ   = 0x28
	regACQ   = 0x30
	doorbell = 0x1000

	ccEnable  = 1 << 0
	ccIOSQES  = 6 << 16 // 64-byte submission entries
	ccIOCQES  = 4 << 20 // 16-byte completion entries
	cstsReady = 1 << 0
	cstsFatal = 1 << 1

	BAR0Bytes = 0x2000
)

// Queues.
const (
	adminQueue   = 0
	ioQueue      = 1
	adminEntries = 16
	ioEntries    = 64
	sqeBytes     = 64
	cqeBytes     = 16
	pageBytes    = 4096
	pagesMax     = 16 // data pages per command: 64 KiB
	slotsMax     = 8
)

// Commands (admin: §5; NVM: NVM Command Set §3).
const (
	opCreateSQ = 0x01
	opCreateCQ = 0x05
	opIdentify = 0x06
	opFlush    = 0x00
	opWrite    = 0x01
	opRead     = 0x02

	cnsNamespace  = 0
	cnsController = 1
	namespace     = 1

	qPhysContig  = 1 << 0
	cqIntEnabled = 1 << 1
)

var le = binary.LittleEndian
