package ata

// multiple.go - READ MULTIPLE (W15-S05). READ SECTORS hands over one sector
// per data request, and QEMU fetches each sector from the image on its own:
// about 340 microseconds a sector, so a 15 MB Qt program took 10 s to load.
// After SET MULTIPLE MODE the drive hands over a block of up to 16 sectors
// per request, read with one string instruction. Drives that report no
// block size (IDENTIFY word 47) keep READ SECTORS. Writes stay one sector
// per request: they are small and rare. Reference: ATA/ATAPI-6, 8.37 and
// 8.47.

import "burrow/kernel/hal"

const (
	cmdReadMultiple = 0xC4
	cmdSetMultiple  = 0xC6
	idMaxMultiple   = 47 // IDENTIFY word: low byte, the largest block
	maxMultiple     = 16
)

// setMultiple turns multiple mode on with the largest block the drive
// offers (up to 16 sectors); c.mu held. It leaves d.multi 0 when the drive
// has none or refuses.
func (d *Disk) setMultiple(id *[256]uint16) {
	n := min(int(id[idMaxMultiple]&0xFF), maxMultiple)
	if n < 2 {
		return
	}
	d.command(0, n, cmdSetMultiple)
	d.ch.settle()
	if d.ch.wait(false) == nil {
		d.multi = n
	}
}

// readMultiple reads n sectors at lba into buf a block at a time; c.mu held.
func (d *Disk) readMultiple(lba uint64, n int, buf []byte) error {
	c := d.ch
	d.command(lba, n, cmdReadMultiple)
	for s := 0; s < n; s += d.multi {
		c.settle()
		if err := c.wait(true); err != nil {
			return err
		}
		block := buf[s*SectorSize : min(s+d.multi, n)*SectorSize]
		c.inData(block)
	}
	return nil
}

// inData reads len(b)/2 words from the data port: one REP INSW when the
// ports can, else a word at a time.
func (c *Channel) inData(b []byte) {
	if str, ok := c.io.(hal.PortIOString); ok {
		str.InWords(ioBase+regData, b)
		return
	}
	for i := 0; i < len(b); i += 2 {
		w := c.io.In16(ioBase + regData)
		b[i], b[i+1] = byte(w), byte(w>>8)
	}
}
