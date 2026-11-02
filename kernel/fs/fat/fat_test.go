package fat

import (
	"bytes"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

var mt = time.Date(2026, 11, 2, 21, 30, 40, 0, time.UTC)

// source covers: long and mixed-case names, nesting, an empty file, a file
// of many clusters, a directory with more entries than one cluster holds.
func source() fstest.MapFS {
	m := fstest.MapFS{
		"hello.txt":                        {Data: []byte("Hello from FAT32\n"), ModTime: mt},
		"empty":                            {ModTime: mt},
		"docs/A file with a long name.TXT": {Data: []byte("long names work\n"), ModTime: mt},
		"docs/deeper/still/here.md":        {Data: []byte("# here\n"), ModTime: mt},
		"big.bin":                          {Data: bytes.Repeat([]byte("0123456789abcdef"), 5000), ModTime: mt},
		"ünïcode name ✓":                   {Data: []byte("utf-16 in the long name\n"), ModTime: mt},
	}
	for i := 0; i < 40; i++ {
		m["many/file-"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune('0'+i/26))] = &fstest.MapFile{Data: []byte{byte(i)}, ModTime: mt}
	}
	return m
}

func image(t *testing.T) []byte {
	t.Helper()
	img := make([]byte, MinImageBytes)
	if err := Format(img, source(), "burrowdata"); err != nil {
		t.Fatal(err)
	}
	return img
}

func TestFormatThenMountConformance(t *testing.T) {
	f, err := Mount(bytes.NewReader(image(t)))
	if err != nil {
		t.Fatal(err)
	}
	if f.Label != "BURROWDATA" {
		t.Errorf("label %q", f.Label)
	}
	var files []string
	for name := range source() {
		files = append(files, name)
	}
	if err := fstest.TestFS(f, files...); err != nil {
		t.Fatal(err)
	}
	for name, want := range source() {
		got, err := fs.ReadFile(f, name)
		if err != nil || !bytes.Equal(got, want.Data) {
			t.Errorf("%s: %d bytes, %v", name, len(got), err)
		}
	}
	st, _ := fs.Stat(f, "hello.txt")
	if !st.ModTime().Equal(mt) {
		t.Errorf("mtime %v, want %v", st.ModTime(), mt)
	}
	if b, err := fs.ReadFile(f, "HELLO.TXT"); err != nil || len(b) == 0 {
		t.Errorf("FAT names are case-insensitive: %v", err)
	}
}

func TestMountRejects(t *testing.T) {
	img := image(t)
	bad := func(edit func(b []byte)) []byte {
		b := append([]byte(nil), img[:sectorSize]...)
		edit(b)
		return append(b, img[sectorSize:]...)
	}
	for name, b := range map[string][]byte{
		"no signature": bad(func(b []byte) { b[sigOffset] = 0 }),
		"FAT16":        bad(func(b []byte) { le.PutUint16(b[bpbFATSz16:], 9) }),
		"bad BPB":      bad(func(b []byte) { b[bpbSecPerClus] = 3 }),
		"too small":    bad(func(b []byte) { le.PutUint32(b[bpbTotSec32:], 4000) }),
	} {
		if _, err := Mount(bytes.NewReader(b)); err == nil {
			t.Errorf("%s: mounted", name)
		}
	}
}

func TestBrokenChainIsAnError(t *testing.T) {
	img := image(t)
	f, err := Mount(bytes.NewReader(img))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := f.lookup("big.bin")
	f.fat[e.first+3] = e.first // a loop
	if _, err := f.Open("big.bin"); err == nil {
		t.Error("opened a file whose chain loops")
	}
	if _, err := f.Open("nope/x"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open(nope/x) = %v", err)
	}
}

func TestFormatRejectsSmallImages(t *testing.T) {
	if err := Format(make([]byte, 1<<20), source(), "x"); err == nil {
		t.Error("Format accepted 1 MiB")
	}
}
