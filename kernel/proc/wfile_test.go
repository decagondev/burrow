package proc

// wfile_test.go - when an open file's writes go back to the file system
// (W24-S03): every change while it is small, by doublings once it is big,
// and all of it at the last close and at fsync.

import (
	"bytes"
	"io/fs"
	"testing"
	"testing/fstest"

	"burrow/kernel/vfs"
)

// countFS is a WritableFS that counts whole-file write-backs.
type countFS struct {
	fstest.MapFS
	writes int
}

func (c *countFS) WriteFile(name string, data []byte) error {
	c.writes++
	c.MapFS[name] = &fstest.MapFile{Data: data, Mode: 0o644}
	return nil
}
func (c *countFS) WriteFileAs(name string, data []byte, _ vfs.NewFile) error {
	return c.WriteFile(name, data)
}
func (c *countFS) MkdirAs(string, vfs.NewFile) error { return fs.ErrPermission }
func (c *countFS) Remove(name string) error          { delete(c.MapFS, name); return nil }

func TestWfileSmallFileWrittenAtEveryChange(t *testing.T) {
	c := &countFS{MapFS: fstest.MapFS{}}
	w := &wfile{fsys: c, name: "f"}
	for i := 0; i < 3; i++ {
		w.Write([]byte("abc"))
		if got := string(c.MapFS["f"].Data); got != string(bytes.Repeat([]byte("abc"), i+1)) {
			t.Fatalf("after write %d the file holds %q", i, got)
		}
	}
}

func TestWfileBigFileWrittenBackByDoublings(t *testing.T) {
	c := &countFS{MapFS: fstest.MapFS{}}
	w := &wfile{fsys: c, name: "f"}
	chunk := bytes.Repeat([]byte{7}, 1024)
	const total = 1 << 20
	for n := 0; n < total; n += len(chunk) {
		if _, err := w.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if c.writes > 64+8 { // 64 while small, then one per doubling
		t.Fatalf("%d write-backs for 1 MiB in 1 KiB writes", c.writes)
	}
	w.Sync()
	if len(c.MapFS["f"].Data) != total {
		t.Fatalf("fsync left %d bytes, want %d", len(c.MapFS["f"].Data), total)
	}
	before := c.writes
	w.Sync()
	if c.writes != before {
		t.Fatal("a second fsync with nothing new wrote again")
	}
	w.WriteAt([]byte{9}, 5)
	w.Close()
	if c.MapFS["f"].Data[5] != 9 {
		t.Fatal("close did not write back the last change")
	}
}
