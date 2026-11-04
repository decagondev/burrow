package ext2_test

// write_test.go - the writer (W19-S02), checked the way FAT's was: the
// same operations on an ext2 volume and on memfs, the trees compared, and
// e2fsck -fn on the image after each sequence; debugfs reads back what the
// kernel wrote. Sequences are seeded and the seed is in every failure, so a
// failure replays (PIT-54).

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"burrow/kernel/fs/ext2"
	"burrow/kernel/fs/memfs"
	"burrow/kernel/vfs"
)

var when = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

// writer is what both file systems offer.
type writer interface {
	fs.FS
	WriteFile(name string, data []byte, mode fs.FileMode, mtime time.Time) error
	Mkdir(name string, mode fs.FileMode, mtime time.Time) error
	Remove(name string) error
	Rename(oldname, newname string) error
}

var names = []string{"a", "b", "c", "d/e", "d/f", "a/x", "a/y", "b/z", "indexed/new", "indexed/" + name(7)}

// sizes cross the direct blocks (12), the single indirect block (268) and
// into the double indirect block with 1 KiB blocks.
var sizes = []int{0, 1, 1023, 1024, 12 << 10, 12<<10 + 1, 100 << 10, 268 << 10, 268<<10 + 5, 400 << 10}

func payload(r *rand.Rand) []byte {
	b := make([]byte, sizes[r.Intn(len(sizes))])
	r.Read(b)
	return b
}

// step applies one random operation to both and says what it was.
func step(r *rand.Rand, e, m writer) (string, error, error) {
	n1, n2 := names[r.Intn(len(names))], names[r.Intn(len(names))]
	switch r.Intn(10) {
	case 0, 1, 2, 3:
		b := payload(r)
		return fmt.Sprintf("write %s (%d bytes)", n1, len(b)), e.WriteFile(n1, b, 0o644, when), m.WriteFile(n1, b, 0o644, when)
	case 4, 5:
		return "mkdir " + n1, e.Mkdir(n1, 0o755, when), m.Mkdir(n1, 0o755, when)
	case 6, 7:
		return "remove " + n1, e.Remove(n1), m.Remove(n1)
	default:
		return "rename " + n1 + " " + n2, e.Rename(n1, n2), m.Rename(n1, n2)
	}
}

// listing lists every file with its type and contents.
func listing(t *testing.T, fsys fs.FS) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			out[p] = "dir"
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		out[p] = fmt.Sprintf("%d:%x", len(b), b[:min(len(b), 16)])
		return err
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return out
}

func diff(a, b map[string]string) string {
	var keys []string
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var d []string
	for _, k := range keys {
		if a[k] != b[k] {
			d = append(d, fmt.Sprintf("%s: ext2 %q memfs %q", k, a[k], b[k]))
		}
	}
	return strings.Join(d, "\n")
}

func TestRandomOperationsMatchMemfs(t *testing.T) {
	tool(t, "mke2fs")
	for seed := int64(1); seed <= 12; seed++ {
		img := blank(t)
		e, done := mountRW(t, img)
		m := memfs.New()
		m.Mkdir("lost+found", 0o700, when) // mke2fs makes it
		m.Mkdir("indexed", 0o755, when)
		for i := 1; i <= 300; i++ {
			m.WriteFile("indexed/"+name(i), []byte("x"), 0o644, when)
		}
		r := rand.New(rand.NewSource(seed))
		var log []string
		for i := 0; i < 80; i++ {
			what, ee, me := step(r, e, m)
			log = append(log, what)
			if (ee == nil) != (me == nil) {
				t.Fatalf("seed %d, step %d (%s): ext2 %v, memfs %v\n%s", seed, i, what, ee, me, strings.Join(log, "\n"))
			}
		}
		if d := diff(listing(t, e), listing(t, m)); d != "" {
			t.Fatalf("seed %d: the trees differ:\n%s\nafter:\n%s", seed, d, strings.Join(log, "\n"))
		}
		done()
		fsck(t, img, fmt.Sprintf("seed %d", seed))
		e2, done2 := mountRW(t, img) // what was written is what a new mount reads
		if d := diff(listing(t, e2), listing(t, m)); d != "" {
			t.Fatalf("seed %d: after remounting:\n%s", seed, d)
		}
		done2()
	}
}

func TestWrittenFilesReadBackWithDebugfs(t *testing.T) {
	img := blank(t)
	e, done := mountRW(t, img)
	big := bigData()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(e.Mkdir("src", 0o750, when))
	must(e.Mkdir("src/deep", 0o755, when))
	must(e.WriteFile("src/deep/big.bin", big, 0o640, when))
	must(e.WriteFile("small.txt", []byte(hello), 0o644, when))
	must(e.MakeSocket("sock", when))
	done()
	fsck(t, img, "after writing")
	dbg := tool(t, "debugfs")
	out, err := exec.Command(dbg, "-R", "cat /src/deep/big.bin", img).Output()
	if err != nil || !bytes.Equal(out, big) {
		t.Errorf("debugfs cat big.bin: %d bytes, %v", len(out), err)
	}
	stat := run(t, dbg, "-R", "stat /src", img)
	for _, want := range []string{"Type: directory", "Mode:  0750", "Links: 3", "2030"} {
		if !strings.Contains(stat, want) {
			t.Errorf("debugfs stat /src lacks %q:\n%s", want, stat)
		}
	}
	if s := run(t, dbg, "-R", "stat /sock", img); !strings.Contains(s, "Type: socket") {
		t.Errorf("sock:\n%s", s)
	}
}

func TestDirectoryGrowsAndShrinks(t *testing.T) {
	img := blank(t)
	e, done := mountRW(t, img)
	e.Mkdir("many", 0o755, when)
	for i := 0; i < 500; i++ { // about 20 blocks of entries: past the direct blocks
		if err := e.WriteFile(fmt.Sprintf("many/a-file-with-a-long-name-%04d", i), []byte{byte(i)}, 0o644, when); err != nil {
			t.Fatalf("file %d: %v", i, err)
		}
	}
	for i := 0; i < 500; i += 2 {
		if err := e.Remove(fmt.Sprintf("many/a-file-with-a-long-name-%04d", i)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 300; i += 3 { // the htree directory, changed: its index goes
		e.Remove("indexed/" + name(i))
	}
	e.WriteFile("indexed/added", nil, 0o644, when)
	ents, _ := fs.ReadDir(e, "many")
	if len(ents) != 250 {
		t.Errorf("%d entries left, want 250", len(ents))
	}
	done()
	fsck(t, img, "after growing and shrinking directories")
}

func TestErrors(t *testing.T) {
	img := blank(t)
	e, done := mountRW(t, img)
	defer done()
	e.Mkdir("d", 0o755, when)
	e.WriteFile("d/f", []byte("x"), 0o644, when)
	for _, c := range []struct {
		what string
		err  error
		want error
	}{
		{"mkdir over a dir", e.Mkdir("d", 0o755, when), fs.ErrExist},
		{"remove a non-empty dir", e.Remove("d"), syscall.ENOTEMPTY},
		{"write over a dir", e.WriteFile("d", nil, 0o644, when), syscall.EISDIR},
		{"write under a file", e.WriteFile("d/f/g", nil, 0o644, when), syscall.ENOTDIR},
		{"remove nothing", e.Remove("nothing"), fs.ErrNotExist},
		{"rename a dir into itself", e.Rename("d", "d/inside"), syscall.EINVAL},
		{"rename a file over a dir", e.Rename("d/f", "d"), syscall.EISDIR},
		{"a name too long", e.WriteFile(strings.Repeat("n", 256), nil, 0o644, when), syscall.ENAMETOOLONG},
		{"write the root", e.WriteFile(".", nil, 0o644, when), fs.ErrInvalid},
	} {
		if !errors.Is(c.err, c.want) {
			t.Errorf("%s: %v, want %v", c.what, c.err, c.want)
		}
	}
}

func TestFullVolume(t *testing.T) {
	img := blank(t)
	e, done := mountRW(t, img)
	big := make([]byte, 3<<20)
	var err error
	for i := 0; err == nil && i < 10; i++ {
		err = e.WriteFile(fmt.Sprintf("fill%d", i), big, 0o644, when)
	}
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("filling an 8 MiB volume: %v, want ENOSPC", err)
	}
	if err := e.Remove("fill0"); err != nil {
		t.Fatal(err)
	}
	if err := e.WriteFile("after", []byte("room again"), 0o644, when); err != nil {
		t.Errorf("after freeing: %v", err)
	}
	done()
	fsck(t, img, "after ENOSPC")
}

func TestHardLinksShareNewContents(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "one"), []byte("old"), 0o644)
	os.Link(filepath.Join(src, "one"), filepath.Join(src, "two"))
	img := image(t, src, "-b", "1024")
	e, done := mountRW(t, img)
	if err := e.WriteFile("one", []byte("new contents"), 0o644, when); err != nil {
		t.Fatal(err)
	}
	if b, _ := fs.ReadFile(e, "two"); string(b) != "new contents" {
		t.Errorf("the other name reads %q", b)
	}
	if err := e.Remove("one"); err != nil {
		t.Fatal(err)
	}
	info, _ := e.Lstat("two")
	if a, _ := vfs.AttrOf(info); a.Nlink != 1 {
		t.Errorf("links after removing one name: %d", a.Nlink)
	}
	done()
	fsck(t, img, "hard links")
}

// TestReadOnlyRefusesWrites: Mount (not MountRW) and volumes with unknown
// read-only-compatible features never write.
func TestReadOnlyRefusesWrites(t *testing.T) {
	img := blank(t)
	fsys, _ := mount(t, img)
	if err := fsys.WriteFile("x", nil, 0o644, when); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("write on a read-only mount: %v", err)
	}
	b, _ := os.ReadFile(img)
	b[1024+100] |= 0x80
	os.WriteFile(img, b, 0o644)
	if _, err := ext2.MountRW(nil); err == nil {
		t.Error("MountRW(nil) worked")
	}
	e, done := mountRW(t, img)
	defer done()
	if err := e.Mkdir("x", 0o755, when); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("write with an unknown ro_compat feature: %v", err)
	}
}
