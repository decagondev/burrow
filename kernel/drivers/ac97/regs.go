// Package ac97 drives an Intel AC'97 audio controller (QEMU's -device AC97,
// PCI 8086:2415; W22-S02): the codec through the mixer's registers (NAM,
// I/O BAR 0) and playback through the bus master's PCM-out channel (NABM,
// I/O BAR 1), a ring of 32 buffers in DMA pages described by a buffer
// descriptor list. It is sound.Output for /dev/dsp. It never records, never
// mixes two programs, and touches the card only through Ports.
// Reference: Intel 82801AA (ICH) datasheet, the AC'97 2.3 codec spec;
// docs/04-plan/waves/WAVE-22-sound-tablet.md.
package ac97

import "burrow/kernel/hal"

const (
	Vendor = 0x8086
	Device = 0x2415

	// Mixer (NAM) registers, 16-bit.
	namReset     = 0x00
	namMaster    = 0x02 // bit 15 mute, then attenuation per channel (0 = loudest)
	namPCMOut    = 0x18
	namExtID     = 0x28 // bit 0: variable rate audio (VRA)
	namExtCtrl   = 0x2A // bit 0: VRA on
	namFrontRate = 0x2C // PCM front DAC rate, Hz

	extVRA        = 1 << 0
	volumeMax     = 0x0000
	volumePCMGain = 0x0808 // PCM out at -12 dB, so a full-scale tone does not clip

	// Bus master (NABM): the PCM-out channel's box, then the global registers.
	poBDBAR = 0x10 // 32-bit: physical address of the descriptor list
	poCIV   = 0x14 // 8-bit: the buffer playing
	poLVI   = 0x15 // 8-bit: the last valid buffer
	poSR    = 0x16 // 16-bit status; bits 2-4 clear when written as 1
	poPICB  = 0x18 // 16-bit: samples left in the buffer playing
	poCR    = 0x1B // 8-bit control
	globCnt = 0x2C // 32-bit
	globSta = 0x30 // 32-bit

	srDCH   = 1 << 0 // the channel has halted
	srLVBCI = 1 << 2 // the last valid buffer completed
	srBCIS  = 1 << 3 // a buffer with IOC completed
	srFIFOE = 1 << 4 // FIFO underrun
	srIRQs  = srLVBCI | srBCIS | srFIFOE

	crRun  = 1 << 0 // run the bus master
	crRR   = 1 << 1 // reset the box's registers
	crLVBI = 1 << 2 // interrupt on the last valid buffer
	crIOCE = 1 << 4 // interrupt on completion of a buffer with IOC

	globColdOff = 1 << 1 // 1: the AC-link is out of cold reset
	staCodecRdy = 1 << 8 // the primary codec is ready

	// The ring: 32 descriptors of 8 bytes, each a DMA page of samples.
	Buffers     = 32
	BufferBytes = 4096 // a page: 1024 stereo 16-bit frames
	descBytes   = 8
	descIOC     = 1 << 15 // interrupt when this buffer completes
	minRate     = 8000
	maxRate     = 48000
	fixedRate   = 48000 // without VRA
)

// Ports are the card's two I/O BARs.
type Ports struct {
	IO8  hal.PortIO
	IO16 hal.PortIO16
	IO32 hal.PortIO32
	NAM  uint16 // BAR 0
	NABM uint16 // BAR 1
}

func (p Ports) mixer(r uint16) uint16    { return p.IO16.In16(p.NAM + r) }
func (p Ports) setMixer(r, v uint16)     { p.IO16.Out16(p.NAM+r, v) }
func (p Ports) in8(r uint16) uint8       { return p.IO8.In8(p.NABM + r) }
func (p Ports) out8(r uint16, v uint8)   { p.IO8.Out8(p.NABM+r, v) }
func (p Ports) in16(r uint16) uint16     { return p.IO16.In16(p.NABM + r) }
func (p Ports) out16(r uint16, v uint16) { p.IO16.Out16(p.NABM+r, v) }
func (p Ports) in32(r uint16) uint32     { return p.IO32.In32(p.NABM + r) }
func (p Ports) out32(r uint16, v uint32) { p.IO32.Out32(p.NABM+r, v) }
