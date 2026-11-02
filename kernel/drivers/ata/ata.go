// Package ata drives IDE disks on the primary channel with PIO and 28-bit
// LBA (E07.F05): IDENTIFY, then READ SECTORS, polling the status register
// with the channel's interrupt switched off (nIEN). Each disk found becomes
// a block.Device named hda (master) or hdb (slave). It is the Go twin of
// the loader's loader/ata_pio.c, plus IDENTIFY and multi-sector reads.
//
// Reference: the OSDev wiki "ATA PIO Mode"; ATA-6 for IDENTIFY words.
package ata

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"burrow/kernel/block"
	"burrow/kernel/driver"
	"burrow/kernel/hal"
)

const (
	SectorSize = 512

	ioBase  uint16 = 0x1F0
	ctlBase uint16 = 0x3F6

	regData    = 0 // 16-bit
	regCount   = 2
	regLBA0    = 3
	regLBA1    = 4
	regLBA2    = 5
	regDrive   = 6
	regStatus  = 7 // read
	regCommand = 7 // write

	stErr  = 0x01
	stDRQ  = 0x08
	stDF   = 0x20
	stBusy = 0x80

	cmdIdentify = 0xEC
	cmdRead     = 0x20
	ctlNoIRQ    = 0x02 // nIEN

	driveLBA   = 0xE0 // LBA mode, the two always-one bits
	driveSlave = 0x10

	maxPerCommand = 256 // a sector count of 0 means 256
	pollLimit     = 1_000_000
	lba28Max      = 1 << 28
)

// Ports is what the driver needs: 8-bit registers and the 16-bit data port.
type Ports interface {
	hal.PortIO
	hal.PortIO16
}

// Channel is one IDE channel; its two drives share the registers.
type Channel struct {
	mu sync.Mutex
	io Ports
}

func NewChannel(io Ports) *Channel { return &Channel{io: io} }

// Disk is one drive on a channel.
type Disk struct {
	ch      *Channel
	slave   bool
	sectors uint64
	Model   string
}

var errTimeout = errors.New("ata: drive did not answer")

func (c *Channel) status() uint8 { return c.io.In8(ioBase + regStatus) }

// settle reads the alternate status 4 times: the 400 ns a drive needs
// after a select or a command before its status means anything.
func (c *Channel) settle() {
	for i := 0; i < 4; i++ {
		c.io.In8(ctlBase)
	}
}

// wait polls until BSY clears and, if data is coming, DRQ is set.
func (c *Channel) wait(data bool) error {
	for i := 0; i < pollLimit; i++ {
		st := c.status()
		switch {
		case st&stBusy != 0:
			continue
		case st&(stErr|stDF) != 0:
			return fmt.Errorf("ata: status %#x, error %#x", st, c.io.In8(ioBase+1))
		case !data || st&stDRQ != 0:
			return nil
		}
	}
	return errTimeout
}

// Identify returns the drive if one answers IDENTIFY as an ATA disk.
func (c *Channel) Identify(slave bool) (*Disk, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.io.Out8(ctlBase, ctlNoIRQ)
	sel := uint8(0xA0)
	if slave {
		sel |= driveSlave
	}
	c.io.Out8(ioBase+regDrive, sel)
	c.settle()
	for _, r := range []uint16{regCount, regLBA0, regLBA1, regLBA2} {
		c.io.Out8(ioBase+r, 0)
	}
	c.io.Out8(ioBase+regCommand, cmdIdentify)
	c.settle()
	if c.status() == 0 {
		return nil, errors.New("ata: no drive")
	}
	for i := 0; i < pollLimit && c.status()&stBusy != 0; i++ {
	}
	if c.io.In8(ioBase+regLBA1) != 0 || c.io.In8(ioBase+regLBA2) != 0 {
		return nil, errors.New("ata: not an ATA disk (ATAPI?)")
	}
	if err := c.wait(true); err != nil {
		return nil, err
	}
	var id [256]uint16
	for i := range id {
		id[i] = c.io.In16(ioBase + regData)
	}
	return &Disk{ch: c, slave: slave, sectors: uint64(id[60]) | uint64(id[61])<<16, Model: model(id[27:47])}, nil
}

// model decodes the IDENTIFY model string: two characters per word, high
// byte first, padded with spaces.
func model(words []uint16) string {
	b := make([]byte, 0, 2*len(words))
	for _, w := range words {
		b = append(b, byte(w>>8), byte(w))
	}
	return strings.TrimSpace(string(b))
}

func (d *Disk) SectorSize() int { return SectorSize }
func (d *Disk) Sectors() uint64 { return d.sectors }

// ReadSectors implements block.Device: one READ SECTORS per 256 sectors.
func (d *Disk) ReadSectors(lba uint64, buf []byte) error {
	if err := block.CheckRange(d, lba, buf); err != nil {
		return err
	}
	if lba+uint64(len(buf)/SectorSize) > lba28Max {
		return block.ErrRange
	}
	c := d.ch
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(buf) > 0 {
		n := min(len(buf)/SectorSize, maxPerCommand)
		if err := d.readChunk(lba, n, buf[:n*SectorSize]); err != nil {
			return fmt.Errorf("ata: read %d sectors at %d: %w", n, lba, err)
		}
		lba += uint64(n)
		buf = buf[n*SectorSize:]
	}
	return nil
}

func (d *Disk) readChunk(lba uint64, n int, buf []byte) error {
	c := d.ch
	sel := uint8(driveLBA) | uint8(lba>>24)&0x0F
	if d.slave {
		sel |= driveSlave
	}
	c.io.Out8(ioBase+regDrive, sel)
	c.settle()
	c.io.Out8(ioBase+regCount, uint8(n)) // 256 wraps to 0, which means 256
	c.io.Out8(ioBase+regLBA0, uint8(lba))
	c.io.Out8(ioBase+regLBA1, uint8(lba>>8))
	c.io.Out8(ioBase+regLBA2, uint8(lba>>16))
	c.io.Out8(ioBase+regCommand, cmdRead)
	for s := 0; s < n; s++ {
		c.settle()
		if err := c.wait(true); err != nil {
			return err
		}
		sector := buf[s*SectorSize : (s+1)*SectorSize]
		for i := 0; i < SectorSize; i += 2 {
			w := c.io.In16(ioBase + regData)
			sector[i], sector[i+1] = byte(w), byte(w>>8)
		}
	}
	return nil
}

func init() { driver.Register(&controller{}) }

type controller struct{}

func (*controller) Name() string { return "ata" }

// Probe: a floating bus (no controller) reads 0xFF.
func (*controller) Probe(env *driver.Env) bool {
	return env.Ports16 != nil && env.Ports.In8(ioBase+regStatus) != 0xFF
}

type ports struct {
	hal.PortIO
	hal.PortIO16
}

func (*controller) Start(env *driver.Env) error {
	ch := NewChannel(ports{env.Ports, env.Ports16})
	found := 0
	for i, name := range []string{"hda", "hdb"} {
		d, err := ch.Identify(i == 1)
		if err != nil {
			continue
		}
		block.Register(name, d)
		env.Log.Infof("%s: %q, %d sectors (%d KiB)", name, d.Model, d.sectors, d.sectors/2)
		found++
	}
	if found == 0 {
		return errors.New("no disks on the primary channel")
	}
	return nil
}
