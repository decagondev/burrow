package part

// part_test.go - a GPT disk as mkimage writes one (its EFI system partition
// named apart), an MBR disk as fdisk writes one (an extended partition
// skipped), a whole-disk FAT volume whose boot sector carries the 0x55AA an
// MBR has, a blank disk, broken MBRs, and partition names.

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"

	"burrow/kernel/block"
	"burrow/kernel/block/blocktest"
	"burrow/kernel/block/gpt"
)

const ss = 512

var dataType = [16]byte{0xA2, 0xA0, 0xD0, 0xEB, 0xE5, 0xB9, 0x33, 0x44, 0x87, 0xC0, 0x68, 0xB6, 0xB7, 0x26, 0x99, 0xC7}

// gptDisk is n sectors with a GPT holding [type, first, last] entries.
func gptDisk(n uint64, parts [][3]any) []byte {
	d := make([]byte, n*ss)
	entries := d[2*ss : 2*ss+128*128]
	for i, p := range parts {
		e := entries[i*128:]
		typ := p[0].([16]byte)
		copy(e[0:16], typ[:])
		e[16] = byte(i + 1)
		binary.LittleEndian.PutUint64(e[32:], p[1].(uint64))
		binary.LittleEndian.PutUint64(e[40:], p[2].(uint64))
	}
	h := d[ss : ss+92]
	copy(h, "EFI PART")
	binary.LittleEndian.PutUint32(h[8:], 0x00010000)
	binary.LittleEndian.PutUint32(h[12:], 92)
	binary.LittleEndian.PutUint64(h[24:], 1)
	binary.LittleEndian.PutUint64(h[32:], n-1)
	binary.LittleEndian.PutUint64(h[40:], 34)
	binary.LittleEndian.PutUint64(h[48:], n-34)
	binary.LittleEndian.PutUint64(h[72:], 2)
	binary.LittleEndian.PutUint32(h[80:], 128)
	binary.LittleEndian.PutUint32(h[84:], 128)
	binary.LittleEndian.PutUint32(h[88:], crc32.ChecksumIEEE(entries))
	binary.LittleEndian.PutUint32(h[16:], crc32.ChecksumIEEE(h))
	return d
}

// mbrDisk is n sectors with an MBR of [status, type, first, count] entries.
func mbrDisk(n uint64, entries [][4]uint32) []byte {
	d := make([]byte, n*ss)
	for i, e := range entries {
		p := d[mbrEntries+i*mbrEntryBytes:]
		p[0], p[4] = byte(e[0]), byte(e[1])
		binary.LittleEndian.PutUint32(p[8:], e[2])
		binary.LittleEndian.PutUint32(p[12:], e[3])
	}
	d[510], d[511] = 0x55, 0xAA
	return d
}

func TestFindGPT(t *testing.T) {
	img := gptDisk(4096, [][3]any{{gpt.ESPType, uint64(2048), uint64(2999)}, {dataType, uint64(3000), uint64(3999)}})
	copy(img[3000*ss:], "the data partition")
	l, err := Find("sda", block.NewRAM(img, ss), nil)
	if err != nil {
		t.Fatal(err)
	}
	if l.Kind != GPT || l.ESP == nil || l.ESP.First != 2048 || len(l.Volumes) != 1 {
		t.Fatalf("layout = %+v", l)
	}
	v := l.Volumes[0]
	if v.Name != "sda2" || v.Number != 2 || v.Dev.Sectors() != 1000 {
		t.Errorf("volume = %+v (%d sectors)", v, v.Dev.Sectors())
	}
	blocktest.Run(t, v.Dev, img[3000*ss:4000*ss])
	if _, ok := v.Dev.(block.Writer); !ok {
		t.Error("a partition of a writable disk is not writable")
	}
}

func TestFindMBR(t *testing.T) {
	img := mbrDisk(4096, [][4]uint32{{0x80, 0x0C, 2048, 1024}, {0, 0x05, 3072, 512}, {0, 0x83, 3584, 512}})
	copy(img[3584*ss:], "linux")
	l, err := Find("nvme0n1", block.NewRAM(img, ss), func(block.Device) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if l.Kind != MBR || l.ESP != nil || len(l.Volumes) != 2 {
		t.Fatalf("layout = %+v", l)
	}
	if l.Volumes[0].Name != "nvme0n1p1" || l.Volumes[1].Name != "nvme0n1p3" {
		t.Errorf("names %q, %q", l.Volumes[0].Name, l.Volumes[1].Name)
	}
	buf := make([]byte, ss)
	if err := l.Volumes[1].Dev.ReadSectors(0, buf); err != nil || !bytes.HasPrefix(buf, []byte("linux")) {
		t.Errorf("partition 3's first sector: %q, %v", buf[:8], err)
	}
}

// A FAT volume's boot sector ends in 55 AA too: the caller's probe says it
// is a file system, so it is one volume, not an MBR.
func TestFindWholeDisk(t *testing.T) {
	img := mbrDisk(64, nil)
	copy(img[82:], "FAT32   ")
	l, err := Find("hdb", block.NewRAM(img, ss), func(block.Device) bool { return true })
	if err != nil || l.Kind != Whole || len(l.Volumes) != 1 || l.Volumes[0].Name != "hdb" {
		t.Errorf("FAT volume: %+v, %v", l, err)
	}
	l, err = Find("vda", block.NewRAM(make([]byte, 64*ss), ss), nil) // blank
	if err != nil || l.Kind != Whole || l.Volumes[0].Number != 0 {
		t.Errorf("blank disk: %+v, %v", l, err)
	}
}

// What is not an MBR is a whole disk: a bad status, an entry past the end,
// a lone protective entry (a GPT whose header is gone).
func TestFindBrokenMBR(t *testing.T) {
	for name, img := range map[string][]byte{
		"status":     mbrDisk(64, [][4]uint32{{0x7F, 0x83, 8, 8}}),
		"past end":   mbrDisk(64, [][4]uint32{{0, 0x83, 60, 8}}),
		"protective": mbrDisk(64, [][4]uint32{{0, 0xEE, 1, 63}}),
		"empty":      mbrDisk(64, nil),
	} {
		l, err := Find("sdb", block.NewRAM(img, ss), nil)
		if err != nil || l.Kind != Whole {
			t.Errorf("%s: %+v, %v", name, l, err)
		}
	}
}

func TestName(t *testing.T) {
	for _, c := range []struct {
		disk string
		n    int
		want string
	}{{"sda", 1, "sda1"}, {"hda", 2, "hda2"}, {"nvme0n1", 3, "nvme0n1p3"}, {"ub0", 1, "ub0p1"}} {
		if got := Name(c.disk, c.n); got != c.want {
			t.Errorf("Name(%q, %d) = %q, want %q", c.disk, c.n, got, c.want)
		}
	}
}
