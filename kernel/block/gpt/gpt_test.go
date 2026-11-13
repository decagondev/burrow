package gpt

// gpt_test.go - tables laid out as tools/mkimage/gpt.go writes them (an EFI
// system partition at LBA 2048), a second partition, a bad header or entry
// CRC, a disk without a GPT, and a partition read as a device.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"testing"
	"unicode/utf16"

	"burrow/kernel/block"
	"burrow/kernel/block/blocktest"
)

const ss = 512

type part struct {
	typ         [16]byte
	first, last uint64
	name        string
}

// disk builds a GPT disk of n sectors holding parts.
func disk(n uint64, parts []part) []byte {
	d := make([]byte, n*ss)
	entries := d[2*ss : 2*ss+128*128]
	for i, p := range parts {
		e := entries[i*128:]
		copy(e[0:16], p.typ[:])
		e[16] = byte(i + 1) // a partition GUID that is not zero
		binary.LittleEndian.PutUint64(e[32:], p.first)
		binary.LittleEndian.PutUint64(e[40:], p.last)
		for j, u := range utf16.Encode([]rune(p.name)) {
			binary.LittleEndian.PutUint16(e[56+2*j:], u)
		}
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

var dataType = [16]byte{0xA2, 0xA0, 0xD0, 0xEB, 0xE5, 0xB9, 0x33, 0x44, 0x87, 0xC0, 0x68, 0xB6, 0xB7, 0x26, 0x99, 0xC7}

func TestReadMkimageLayout(t *testing.T) {
	img := disk(4096, []part{{ESPType, 2048, 3000, "EFI system partition"}, {dataType, 3001, 4000, "data"}})
	copy(img[2048*ss:], "boot partition's first sector")
	tbl, err := Read(block.NewRAM(img, ss))
	if err != nil {
		t.Fatal(err)
	}
	if len(tbl) != 2 {
		t.Fatalf("%d partitions, want 2", len(tbl))
	}
	esp := tbl[0]
	if !esp.IsESP() || esp.First != 2048 || esp.Last != 3000 || esp.Name != "EFI system partition" {
		t.Errorf("partition 0 = %+v", esp)
	}
	if tbl[1].IsESP() || tbl[1].Name != "data" {
		t.Errorf("partition 1 = %+v", tbl[1])
	}
	if p, ok := tbl.ESP(); !ok || p.First != 2048 {
		t.Errorf("ESP() = %+v, %v", p, ok)
	}
	dev := esp.Device(block.NewRAM(img, ss))
	if dev.Sectors() != 953 {
		t.Errorf("partition device: %d sectors, want 953", dev.Sectors())
	}
	buf := make([]byte, ss)
	if err := dev.ReadSectors(0, buf); err != nil || !bytes.HasPrefix(buf, []byte("boot partition's")) {
		t.Errorf("sector 0 of the partition: %q, %v", buf[:16], err)
	}
	if err := dev.ReadSectors(953, buf); err == nil {
		t.Error("read past the partition's end accepted")
	}
}

func TestPartitionDeviceConformance(t *testing.T) {
	img := disk(256, []part{{dataType, 64, 127, "x"}})
	copy(img[64*ss:128*ss], blocktest.Pattern(64, ss))
	tbl, err := Read(block.NewRAM(img, ss))
	if err != nil {
		t.Fatal(err)
	}
	blocktest.Run(t, tbl[0].Device(block.NewRAM(img, ss)), blocktest.Pattern(64, ss))
}

func TestReadRefuses(t *testing.T) {
	if _, err := Read(block.NewRAM(make([]byte, 64*ss), ss)); !errors.Is(err, ErrNotGPT) {
		t.Errorf("blank disk: %v, want ErrNotGPT", err)
	}
	img := disk(4096, []part{{ESPType, 2048, 3000, "esp"}})
	img[ss+40]++ // the header's first usable LBA, so its CRC is wrong
	if _, err := Read(block.NewRAM(img, ss)); err == nil || errors.Is(err, ErrNotGPT) {
		t.Errorf("bad header CRC: %v", err)
	}
	img = disk(4096, []part{{ESPType, 2048, 3000, "esp"}})
	img[2*ss+60]++ // a name byte: the entries' CRC is wrong
	if _, err := Read(block.NewRAM(img, ss)); err == nil {
		t.Error("bad entries CRC accepted")
	}
	img = disk(4096, []part{{ESPType, 3000, 2048, "backwards"}})
	if _, err := Read(block.NewRAM(img, ss)); err == nil {
		t.Error("a partition ending before it starts accepted")
	}
	img = disk(4096, []part{{ESPType, 2048, 5000, "past the end"}})
	if _, err := Read(block.NewRAM(img, ss)); err == nil {
		t.Error("a partition past the disk accepted")
	}
}
