package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"
)

func TestBuildGPT(t *testing.T) {
	esp := bytes.Repeat([]byte{0xE5}, 4096*SectorSize)
	disk, err := BuildGPT(esp)
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	total := uint64(len(disk) / SectorSize)
	if total != 2048+4096+33 {
		t.Fatalf("%d sectors", total)
	}
	mbr := disk[:SectorSize]
	if mbr[446+4] != 0xEE || le.Uint32(mbr[446+8:]) != 1 || le.Uint32(mbr[446+12:]) != uint32(total-1) ||
		mbr[510] != 0x55 || mbr[511] != 0xAA {
		t.Fatalf("protective MBR % x", mbr[446:462])
	}
	check := func(lba, alt, table uint64) []byte {
		h := append([]byte(nil), disk[lba*SectorSize:lba*SectorSize+gptHeaderSize]...)
		if string(h[:8]) != "EFI PART" || le.Uint64(h[24:]) != lba || le.Uint64(h[32:]) != alt || le.Uint64(h[72:]) != table {
			t.Fatalf("header at %d: % x", lba, h[:40])
		}
		crc := le.Uint32(h[16:])
		le.PutUint32(h[16:], 0)
		if crc32.ChecksumIEEE(h) != crc {
			t.Fatalf("header at %d: CRC", lba)
		}
		entries := disk[table*SectorSize : table*SectorSize+gptEntries*gptEntrySize]
		if crc32.ChecksumIEEE(entries) != le.Uint32(h[88:]) {
			t.Fatalf("entries at %d: CRC", table)
		}
		if le.Uint64(h[40:]) != 34 || le.Uint64(h[48:]) != 2048+4096-1 {
			t.Fatalf("usable %d..%d", le.Uint64(h[40:]), le.Uint64(h[48:]))
		}
		return entries
	}
	primary := check(1, total-1, 2)
	backup := check(total-1, 1, total-33)
	if !bytes.Equal(primary, backup) {
		t.Fatal("backup table differs")
	}
	e := primary[:gptEntrySize]
	if !bytes.Equal(e[:16], espTypeGUID[:]) || le.Uint64(e[32:]) != 2048 || le.Uint64(e[40:]) != 2048+4096-1 {
		t.Fatalf("entry % x", e[:48])
	}
	if e[16+7]>>4 != 4 || e[16+8]>>6 != 2 {
		t.Fatal("partition GUID is not version 4, RFC 4122 variant")
	}
	if !bytes.Equal(disk[2048*SectorSize:(2048+4096)*SectorSize], esp) {
		t.Fatal("partition bytes moved")
	}
	again, _ := BuildGPT(esp)
	if !bytes.Equal(disk, again) {
		t.Fatal("not reproducible")
	}
	if _, err := BuildGPT(esp[:100]); err == nil {
		t.Fatal("a partial sector accepted")
	}
}

func TestESPFiles(t *testing.T) {
	files, m, err := ESPFiles([]byte("MZ"), []Payload{{"nucleus", []byte("elf")}, {"kargs", []byte("login")}})
	if err != nil {
		t.Fatal(err)
	}
	if string(files[ESPLoaderPath]) != "MZ" || string(files["burrow/nucleus"]) != "elf" || string(files["burrow/kargs"]) != "login" {
		t.Fatalf("%v", files)
	}
	var back Manifest
	if err := back.UnmarshalBinary(files[ESPManifestPath]); err != nil {
		t.Fatal(err)
	}
	if len(back.Entries) != 2 || back.Entries[0].Name != "nucleus" || back.Entries[1].CRC32 != crc32.ChecksumIEEE([]byte("login")) ||
		back.Entries[1].SizeBytes != 5 || len(m.Entries) != 2 {
		t.Fatalf("%+v", back.Entries)
	}
	if _, _, err := ESPFiles(nil, []Payload{{"manifest", nil}}); err == nil {
		t.Fatal("a payload named manifest accepted")
	}
}
