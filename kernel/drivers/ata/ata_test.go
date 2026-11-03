package ata

import (
	"bytes"
	"testing"

	"burrow/kernel/block"
	"burrow/kernel/block/blocktest"
)

// fakeChannel is an IDE channel with up to two disks that implements the
// PIO protocol the driver uses: IDENTIFY, READ SECTORS and WRITE SECTORS
// (LBA28), with DRQ set while a sector waits in the data port or the drive
// waits for one; FLUSH CACHE is counted.
type fakeChannel struct {
	disks    [2][]byte // nil = no drive
	models   [2]string
	regs     [8]uint8
	cur      int // selected drive
	out      []uint16
	reading  int // sectors still to deliver for the current READ
	writing  int // sectors still to take for the current WRITE
	in       []uint16
	nextLBA  uint64
	commands int
	flushes  int
	multi    int   // SET MULTIPLE MODE's block size
	reads    []int // the read commands, in order
}

func (f *fakeChannel) drive() []byte { return f.disks[f.cur] }

func (f *fakeChannel) In8(port uint16) uint8 {
	switch {
	case port == ctlBase:
		return f.In8(ioBase + regStatus)
	case port == ioBase+regStatus:
		if f.drive() == nil {
			return 0
		}
		if len(f.out) > 0 || f.writing > 0 {
			return 0x40 | stDRQ
		}
		return 0x40
	case port > ioBase && port < ioBase+8:
		return f.regs[port-ioBase]
	}
	return 0xFF
}

func (f *fakeChannel) Out8(port uint16, v uint8) {
	switch {
	case port == ioBase+regDrive:
		f.regs[regDrive] = v
		f.cur = int(v>>4) & 1
	case port == ioBase+regCommand:
		f.command(v)
	case port > ioBase && port < ioBase+8:
		f.regs[port-ioBase] = v
	}
}

func (f *fakeChannel) command(cmd uint8) {
	d := f.drive()
	if d == nil {
		return
	}
	f.commands++
	switch cmd {
	case cmdIdentify:
		id := make([]uint16, 256)
		n := len(d) / SectorSize
		id[60], id[61] = uint16(n), uint16(n>>16)
		id[47] = 0x8000 | 16 // READ/WRITE MULTIPLE, blocks of up to 16 sectors
		m := []byte(f.models[f.cur] + "                                        ")[:40]
		for i := 0; i < 20; i++ {
			id[27+i] = uint16(m[2*i])<<8 | uint16(m[2*i+1])
		}
		f.out = id
	case cmdSetMultiple:
		f.multi = int(f.regs[regCount])
	case cmdRead, cmdReadMultiple, cmdWrite:
		count := int(f.regs[regCount])
		if count == 0 {
			count = 256
		}
		f.nextLBA = uint64(f.regs[regLBA0]) | uint64(f.regs[regLBA1])<<8 |
			uint64(f.regs[regLBA2])<<16 | uint64(f.regs[regDrive]&0x0F)<<24
		if cmd != cmdWrite {
			f.reads = append(f.reads, int(cmd))
			f.reading = count
			f.loadSector()
		} else {
			f.writing, f.in = count, nil
		}
	case cmdFlush:
		f.flushes++
	}
}

func (f *fakeChannel) loadSector() {
	if f.reading == 0 {
		return
	}
	s := f.drive()[f.nextLBA*SectorSize:][:SectorSize]
	for i := 0; i < SectorSize; i += 2 {
		f.out = append(f.out, uint16(s[i])|uint16(s[i+1])<<8)
	}
	f.reading--
	f.nextLBA++
}

func (f *fakeChannel) In16(port uint16) uint16 {
	if port != ioBase+regData || len(f.out) == 0 {
		return 0xFFFF
	}
	w := f.out[0]
	f.out = f.out[1:]
	if len(f.out) == 0 {
		f.loadSector()
	}
	return w
}

func (f *fakeChannel) Out16(port uint16, w uint16) {
	if port != ioBase+regData || f.writing == 0 {
		return
	}
	f.in = append(f.in, w)
	if len(f.in) < SectorSize/2 {
		return
	}
	s := f.drive()[f.nextLBA*SectorSize:][:SectorSize]
	for i, w := range f.in {
		s[2*i], s[2*i+1] = byte(w), byte(w>>8)
	}
	f.in = f.in[:0]
	f.writing--
	f.nextLBA++
}

func TestIdentifyAndConformance(t *testing.T) {
	master := blocktest.Pattern(600, SectorSize) // more than one 256-sector command
	f := &fakeChannel{models: [2]string{"QEMU HARDDISK", ""}}
	f.disks[0] = master
	ch := NewChannel(f)
	d, err := ch.Identify(false)
	if err != nil {
		t.Fatal(err)
	}
	if d.Model != "QEMU HARDDISK" || d.Sectors() != 600 {
		t.Errorf("Identify = %q, %d sectors", d.Model, d.Sectors())
	}
	if _, err := ch.Identify(true); err == nil {
		t.Error("Identify found a slave that is not there")
	}
	f.commands = 0
	blocktest.Run(t, d, master)
	whole := make([]byte, 600*SectorSize)
	f.commands = 0
	if err := d.ReadSectors(0, whole); err != nil || f.commands != 3 {
		t.Errorf("600 sectors took %d READ commands (%v), want 3", f.commands, err)
	}
	if f.multi != 16 || f.reads[len(f.reads)-1] != cmdReadMultiple {
		t.Errorf("multiple mode %d, last read command %#x: want 16 and READ MULTIPLE", f.multi, f.reads[len(f.reads)-1])
	}
}

func TestSlaveDisk(t *testing.T) {
	f := &fakeChannel{models: [2]string{"A", "B"}}
	f.disks[0] = blocktest.Pattern(8, SectorSize)
	f.disks[1] = blocktest.Pattern(16, SectorSize)[512:] // different contents
	d, err := NewChannel(f).Identify(true)
	if err != nil {
		t.Fatal(err)
	}
	blocktest.Run(t, d, f.disks[1])
	var _ block.Device = d
}

func TestWriterConformance(t *testing.T) {
	f := &fakeChannel{models: [2]string{"QEMU HARDDISK", ""}}
	f.disks[0] = make([]byte, 600*SectorSize)
	d, err := NewChannel(f).Identify(false)
	if err != nil {
		t.Fatal(err)
	}
	var w block.Writer = d
	blocktest.RunWriter(t, w)
	if f.flushes == 0 {
		t.Error("Flush sent no FLUSH CACHE")
	}
	big := blocktest.Pattern(300, SectorSize)
	f.commands = 0
	if err := d.WriteSectors(100, big); err != nil || f.commands != 2 {
		t.Errorf("300 sectors took %d WRITE commands (%v), want 2", f.commands, err)
	}
	if !bytes.Equal(f.disks[0][100*SectorSize:400*SectorSize], big) {
		t.Error("the disk does not hold the 300 sectors written")
	}
}

// stringChannel is the fake with string I/O, as the real ports have
// (W15-S05): the driver must read and write the same bytes through it.
type stringChannel struct{ *fakeChannel }

func (s stringChannel) InWords(port uint16, dst []byte) {
	for i := 0; i+1 < len(dst); i += 2 {
		w := s.In16(port)
		dst[i], dst[i+1] = byte(w), byte(w>>8)
	}
}

func (s stringChannel) OutWords(port uint16, src []byte) {
	for i := 0; i+1 < len(src); i += 2 {
		s.Out16(port, uint16(src[i])|uint16(src[i+1])<<8)
	}
}

func TestStringIO(t *testing.T) {
	master := blocktest.Pattern(300, SectorSize)
	f := &fakeChannel{models: [2]string{"QEMU HARDDISK", ""}}
	f.disks[0] = master
	d, err := NewChannel(stringChannel{f}).Identify(false)
	if err != nil {
		t.Fatal(err)
	}
	blocktest.Run(t, d, master)
	blocktest.RunWriter(t, d)
}
