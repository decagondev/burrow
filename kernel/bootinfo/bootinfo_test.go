package bootinfo

import (
	"strings"
	"testing"
)

// fake lays the C structs out byte by byte, as the loader does.
func fakeInfo(version uint32, modules int) []byte {
	b := make([]byte, InfoBytes)
	le.PutUint64(b[offMagic:], Magic)
	le.PutUint32(b[offVersion:], version)
	le.PutUint32(b[offBootDrive:], 0x80)
	le.PutUint64(b[offPhysmap:], 0xFFFF800000000000)
	le.PutUint64(b[offModulesAddr:], 0x1100)
	le.PutUint32(b[offModuleCount:], uint32(modules))
	return b
}

func fakeModule(name string, phys, size uint64) []byte {
	e := make([]byte, ModuleBytes)
	copy(e, name)
	le.PutUint64(e[16:], phys)
	le.PutUint64(e[24:], size)
	return e
}

func TestParse(t *testing.T) {
	info, err := Parse(fakeInfo(Version, 3))
	if err != nil {
		t.Fatal(err)
	}
	want := Info{BootDrive: 0x80, PhysmapBase: 0xFFFF800000000000, ModulesAddr: 0x1100, ModuleCount: 3}
	if info != want {
		t.Errorf("Parse = %+v, want %+v", info, want)
	}
	for _, c := range []struct {
		name string
		b    []byte
		want string
	}{
		{"short", make([]byte, 16), "bytes"},
		{"magic", make([]byte, InfoBytes), "magic"},
		{"version 2", fakeInfo(2, 0), "version"}, // before W21-S02: a mixed image
		{"too many modules", fakeInfo(Version, 17), "modules"},
	} {
		if _, err := Parse(c.b); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one mentioning %q", c.name, err, c.want)
		}
	}
}

func TestParseModulesAndFind(t *testing.T) {
	var tab []byte
	tab = append(tab, fakeModule("kernel", 0x420000, 1572864)...)
	tab = append(tab, fakeModule("initrd", 0x5a0000, 10240)...)
	tab = append(tab, fakeModule("loader-frames0", 0x400000, 0x1d000)...) // 14 chars + NUL padding
	mods, err := ParseModules(tab, 3)
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := Find(mods, "initrd"); !ok || m.Phys != 0x5a0000 || m.Size != 10240 {
		t.Errorf("Find(initrd) = %+v, %v", m, ok)
	}
	if mods[2].Name != "loader-frames0" {
		t.Errorf("name = %q", mods[2].Name)
	}
	if _, ok := Find(mods, "nope"); ok {
		t.Error("Find(nope) found something")
	}
	if _, err := ParseModules(tab, 4); err == nil {
		t.Error("ParseModules read past the table")
	}
}

// W21-S02: the framebuffer fields at the offsets boot_info.h asserts (0x30-0x47,
// 0x60-0x66), in the order stage2 copies them from VBE's ModeInfoBlock.
func TestParseFramebuffer(t *testing.T) {
	b := fakeInfo(Version, 1)
	if fb := mustParse(t, b).Framebuffer; fb != (Framebuffer{}) {
		t.Fatalf("no source: %+v, want none", fb)
	}
	le.PutUint64(b[0x30:], 0xFD000000)
	le.PutUint32(b[0x38:], 1280)
	le.PutUint32(b[0x3C:], 1024)
	le.PutUint32(b[0x40:], 5248)
	le.PutUint32(b[0x44:], 32)
	copy(b[0x60:], []byte{8, 16, 8, 8, 8, 0})
	b[0x66] = FbSourceVBE
	want := Framebuffer{Source: 1, Phys: 0xFD000000, Width: 1280, Height: 1024, Pitch: 5248, BPP: 32,
		Red: Field{8, 16}, Green: Field{8, 8}, Blue: Field{8, 0}}
	if fb := mustParse(t, b).Framebuffer; fb != want {
		t.Errorf("framebuffer = %+v, want %+v", fb, want)
	}
}

func mustParse(t *testing.T, b []byte) Info {
	t.Helper()
	info, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return info
}
