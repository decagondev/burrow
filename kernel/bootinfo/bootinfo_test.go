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
	info, err := Parse(fakeInfo(2, 3))
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
		{"version 1", fakeInfo(1, 0), "version"},
		{"too many modules", fakeInfo(2, 17), "modules"},
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
