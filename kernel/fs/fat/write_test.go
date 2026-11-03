package fat

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"
	"testing/fstest"
)

// memDisk is a writable volume in memory, counting flushes.
type memDisk struct {
	b       []byte
	flushes int
}

func (m *memDisk) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off+int64(len(p)) > int64(len(m.b)) {
		return 0, errors.New("out of range")
	}
	return copy(p, m.b[off:]), nil
}

func (m *memDisk) WriteAt(p []byte, off int64) (int, error) {
	if off < 0 || off+int64(len(p)) > int64(len(m.b)) {
		return 0, errors.New("out of range")
	}
	return copy(m.b[off:], p), nil
}

func (m *memDisk) Flush() error { m.flushes++; return nil }

func writable(t *testing.T) (*FS, *memDisk) {
	t.Helper()
	d := &memDisk{b: image(t)}
	f, err := MountRW(d)
	if err != nil {
		t.Fatal(err)
	}
	return f, d
}

// remount reads the image back with a fresh, read-only mount.
func remount(t *testing.T, d *memDisk) *FS {
	t.Helper()
	f, err := Mount(bytes.NewReader(d.b))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func mustRead(t *testing.T, f fs.FS, name string) []byte {
	t.Helper()
	b, err := fs.ReadFile(f, name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

func TestWriteNewFilesAndDirectories(t *testing.T) {
	f, d := writable(t)
	big := bytes.Repeat([]byte("burrow "), 3000) // many clusters
	for _, c := range []struct {
		name string
		data []byte
	}{{"notes.txt", []byte("first note\n")}, {"A long name for a C file.c", []byte("int main(void) { return 0; }\n")},
		{"big.dat", big}, {"empty.txt", nil}} {
		if err := f.WriteFile(c.name, c.data, 0o644, mt); err != nil {
			t.Fatalf("WriteFile(%s): %v", c.name, err)
		}
	}
	if err := f.Mkdir("src", 0o755, mt); err != nil {
		t.Fatal(err)
	}
	if err := f.Mkdir("src/lua", 0o755, mt); err != nil {
		t.Fatal(err)
	}
	if err := f.WriteFile("src/lua/hello.lua", []byte("print('hi')\n"), 0o644, mt); err != nil {
		t.Fatal(err)
	}
	if d.flushes == 0 {
		t.Error("no flush after writing")
	}
	r := remount(t, d)
	if err := fstest.TestFS(r, "notes.txt", "A long name for a C file.c", "big.dat", "empty.txt",
		"src/lua/hello.lua", "hello.txt", "docs/deeper/still/here.md"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mustRead(t, r, "big.dat"), big) || string(mustRead(t, r, "src/lua/hello.lua")) != "print('hi')\n" {
		t.Error("contents differ after a fresh mount")
	}
	if err := f.Mkdir("src", 0o755, mt); !errors.Is(err, fs.ErrExist) {
		t.Errorf("Mkdir of an existing name = %v, want ErrExist", err)
	}
	if err := f.WriteFile("src", []byte("x"), 0o644, mt); !errors.Is(err, syscall.EISDIR) {
		t.Errorf("WriteFile over a directory = %v, want EISDIR", err)
	}
	if err := f.WriteFile("nowhere/x", []byte("x"), 0o644, mt); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("WriteFile in a missing directory = %v, want ErrNotExist", err)
	}
}

func TestOverwriteGrowsAndShrinks(t *testing.T) {
	f, d := writable(t)
	free0 := f.freeClusters()
	for _, data := range [][]byte{
		bytes.Repeat([]byte("a"), 10000), // grows hello.txt's one cluster to many
		[]byte("short again\n"),          // shrinks: the rest goes back
		nil,                              // empty: no cluster at all
		[]byte("once more\n"),
	} {
		if err := f.WriteFile("hello.txt", data, 0o644, mt); err != nil {
			t.Fatal(err)
		}
		if got := mustRead(t, remount(t, d), "hello.txt"); !bytes.Equal(got, data) {
			t.Fatalf("after writing %d bytes the file holds %d", len(data), len(got))
		}
	}
	if f.freeClusters() != free0 {
		t.Errorf("free clusters %d, want %d: the overwrites leaked or double-freed", f.freeClusters(), free0)
	}
	if strictly := remount(t, d).freeClusters(); strictly != free0 {
		t.Errorf("on disk %d free clusters, in memory %d", strictly, free0)
	}
}

// A directory grows past one cluster (16 entries at 512-byte clusters).
func TestDirectoryGrows(t *testing.T) {
	f, d := writable(t)
	f.Mkdir("lots", 0o755, mt)
	var names []string
	for i := 0; i < 40; i++ {
		n := fmt.Sprintf("lots/file number %02d.txt", i)
		names = append(names, n)
		if err := f.WriteFile(n, []byte(n), 0o644, mt); err != nil {
			t.Fatal(err)
		}
	}
	r := remount(t, d)
	if err := fstest.TestFS(r, names...); err != nil {
		t.Fatal(err)
	}
	if string(mustRead(t, r, names[39])) != names[39] {
		t.Error("the last file holds the wrong data")
	}
}

func TestRemove(t *testing.T) {
	f, d := writable(t)
	free0 := f.freeClusters()
	f.Mkdir("tmp", 0o755, mt)
	f.WriteFile("tmp/a.txt", []byte("a"), 0o644, mt)
	if err := f.Remove("tmp"); !errors.Is(err, syscall.ENOTEMPTY) {
		t.Errorf("Remove of a full directory = %v, want ENOTEMPTY", err)
	}
	for _, n := range []string{"tmp/a.txt", "tmp", "big.bin"} {
		if err := f.Remove(n); err != nil {
			t.Fatalf("Remove(%s): %v", n, err)
		}
	}
	r := remount(t, d)
	for _, n := range []string{"tmp", "big.bin"} {
		if _, err := fs.Stat(r, n); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s still there: %v", n, err)
		}
	}
	if err := fstest.TestFS(r, "hello.txt", "docs/A file with a long name.TXT"); err != nil {
		t.Fatal(err)
	}
	bigClusters := (5000*16 + 511) / 512
	if got := f.freeClusters(); got != free0+bigClusters {
		t.Errorf("free clusters %d, want %d (big.bin's back, tmp's back)", got, free0+bigClusters)
	}
	if err := f.Remove("nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Remove of nothing = %v", err)
	}
	// a name freed by Remove can be used again
	if err := f.WriteFile("big.bin", []byte("small now"), 0o644, mt); err != nil {
		t.Fatal(err)
	}
	if string(mustRead(t, remount(t, d), "big.bin")) != "small now" {
		t.Error("re-created file holds the wrong data")
	}
}

func TestReadOnlyMountRefusesWrites(t *testing.T) {
	f := remount(t, &memDisk{b: image(t)})
	if err := f.WriteFile("x", nil, 0o644, mt); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("WriteFile on a read-only mount = %v", err)
	}
}

func TestFullVolume(t *testing.T) {
	f, _ := writable(t)
	huge := make([]byte, (len(f.fat)+10)*int(f.clusterBytes))
	if err := f.WriteFile("huge", huge, 0o644, mt); !errors.Is(err, syscall.ENOSPC) {
		t.Errorf("writing more than the volume = %v, want ENOSPC", err)
	}
	if _, err := fs.Stat(f, "huge"); !errors.Is(err, fs.ErrNotExist) {
		t.Error("a failed write left an entry behind")
	}
}

func TestRename(t *testing.T) {
	f, d := writable(t)
	f.Mkdir("src", 0o755, mt)
	f.Mkdir("src/old", 0o755, mt)
	f.WriteFile("src/old/main.c", []byte("int main;"), 0o644, mt)
	f.WriteFile("draft.txt", []byte("draft"), 0o644, mt)
	for _, c := range []struct {
		from, to string
		want     error
	}{
		{"draft.txt", "src/final text.txt", nil},          // across directories, a long name
		{"hello.txt", "src/final text.txt", nil},          // replaces a file
		{"src/final text.txt", "SRC/FINAL TEXT.TXT", nil}, // only the case changes
		{"src/old", "src/old/inner", syscall.EINVAL},
		{"src/FINAL TEXT.TXT", "src/old", syscall.EISDIR},
		{"src/old", "big.bin", syscall.ENOTDIR},
		{"src/old", "docs", syscall.ENOTEMPTY},
		{"nope", "x", fs.ErrNotExist},
		{"src/old", "moved", nil}, // a directory to the root: its ".." changes
	} {
		if err := f.Rename(c.from, c.to); !errors.Is(err, c.want) && (c.want != nil || err != nil) {
			t.Errorf("Rename(%s, %s) = %v, want %v", c.from, c.to, err, c.want)
		}
	}
	r := remount(t, d)
	if err := fstest.TestFS(r, "src/FINAL TEXT.TXT", "moved/main.c", "big.bin", "docs/deeper/still/here.md"); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, r, "src/FINAL TEXT.TXT")); got != "Hello from FAT32\n" {
		t.Errorf("the replaced file holds %q", got)
	}
	for _, gone := range []string{"draft.txt", "hello.txt", "src/old"} {
		if _, err := fs.Stat(r, gone); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is still there", gone)
		}
	}
	dotdot := func(dirCluster uint32) uint32 { // the ".." entry of a directory
		e := d.b[f.clusterOffset(dirCluster)+dirEntry:]
		return uint32(le.Uint16(e[dirClusHi:]))<<16 | uint32(le.Uint16(e[dirClusLo:]))
	}
	moved, _ := f.lookup("moved")
	if dotdot(moved.first) != 0 {
		t.Errorf("moved/.. points at cluster %d, want 0 (the root)", dotdot(moved.first))
	}
}
