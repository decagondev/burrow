package main

import (
	"bytes"
	"hash/crc32"
	"testing"
)

func fakeStage1() []byte {
	b := make([]byte, SectorSize)
	b[510], b[511] = 0x55, 0xAA
	return b
}

func TestManifestRoundTrip(t *testing.T) {
	in := Manifest{Entries: []Entry{
		{Name: "stage2", LBA: 2, Sectors: 32, SizeBytes: 16384, CRC32: 0xDEADBEEF},
		{Name: "loader", LBA: 34, Sectors: 3, SizeBytes: 1500, CRC32: 0x12345678},
	}}
	raw, err := in.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != SectorSize {
		t.Fatalf("manifest is %d bytes, want %d", len(raw), SectorSize)
	}
	var out Manifest
	if err := out.UnmarshalBinary(raw); err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 2 || out.Entries[1] != in.Entries[1] {
		t.Fatalf("round trip mismatch: %+v", out.Entries)
	}
}

// Guards the C/Go/asm ABI: if this fails, include/burrow/manifest.h and
// boot/boot_abi.inc must be updated in the same commit.
func TestManifestLayoutMatchesC(t *testing.T) {
	m := Manifest{Entries: []Entry{{Name: "loader", LBA: 0x01020304, Sectors: 0x0A0B0C0D}}}
	raw, _ := m.MarshalBinary()
	if string(raw[0:8]) != "BRWMNFST" {
		t.Fatal("magic at offset 0")
	}
	if raw[10] != 1 {
		t.Fatal("count at offset 10")
	}
	if !bytes.Equal(raw[16:22], []byte("loader")) {
		t.Fatal("first entry name at offset 16")
	}
	if raw[16+16] != 0x04 || raw[16+20] != 0x0D {
		t.Fatal("lba at entry+16, sectors at entry+20 (little-endian)")
	}
}

func TestBuildImagePlacesPayloads(t *testing.T) {
	loader := bytes.Repeat([]byte{0xAB}, 1000) // 2 sectors
	kernel := bytes.Repeat([]byte{0xCD}, 512)  // 1 sector
	img, m, err := BuildImage(fakeStage1(), []byte("S2"), []Payload{
		{"loader", loader}, {"kernel", kernel},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if m.Entries[1].LBA != FirstPayloadLBA || m.Entries[2].LBA != FirstPayloadLBA+2 {
		t.Fatalf("unexpected layout: %+v", m.Entries)
	}
	got := img[FirstPayloadLBA*SectorSize : FirstPayloadLBA*SectorSize+1000]
	if !bytes.Equal(got, loader) {
		t.Fatal("loader bytes not at its LBA")
	}
	if m.Entries[1].CRC32 != crc32.ChecksumIEEE(loader) {
		t.Fatal("crc mismatch")
	}
}

func TestBuildImageRejectsBadInputs(t *testing.T) {
	cases := map[string]func() error{
		"short stage1": func() error { _, _, e := BuildImage(make([]byte, 100), nil, nil, 0); return e },
		"no signature": func() error { _, _, e := BuildImage(make([]byte, 512), nil, nil, 0); return e },
		"huge stage2": func() error {
			_, _, e := BuildImage(fakeStage1(), make([]byte, Stage2Sectors*SectorSize+1), nil, 0)
			return e
		},
		"duplicate": func() error {
			_, _, e := BuildImage(fakeStage1(), nil, []Payload{{"a", nil}, {"a", nil}}, 0)
			return e
		},
		"long name": func() error {
			_, _, e := BuildImage(fakeStage1(), nil, []Payload{{"this-name-is-too-long", nil}}, 0)
			return e
		},
	}
	for name, f := range cases {
		if f() == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
