package ext2_test

// ext2_test.go - the reader against volumes e2fsprogs made (W19-S01): never
// against volumes this package made, so the reader and a later writer
// cannot agree on a mistake. Skips without mke2fs, debugfs and e2fsck
// unless BURROW_EXT2_REQUIRED is set (make ext2-interop sets it).

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/fstest"

	"burrow/kernel/fs/ext2"
	"burrow/kernel/vfs"
)

const (
	hello     = "hello, ext2\n"
	sparseLen = 70 << 20 // with 1 KiB blocks, its last block sits behind the triple indirect block
	midOff    = 5 << 20  // behind the double indirect block
	bigLen    = 300 << 10
	longFiles = 400
)

var slowTarget = strings.Repeat("./", 40) + "hello.txt" // longer than the 60 bytes of a fast link

// tool finds an e2fsprogs program (they live in /sbin, often off PATH).
func tool(t *testing.T, name string) string {
	t.Helper()
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, dir := range []string{"/usr/sbin", "/sbin"} {
		if p := filepath.Join(dir, name); fileExists(p) {
			return p
		}
	}
	if os.Getenv("BURROW_EXT2_REQUIRED") != "" {
		t.Fatalf("%s not found (install e2fsprogs)", name)
	}
	t.Skipf("%s not found (install e2fsprogs to run the ext2 tests)", name)
	return ""
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func run(t *testing.T, prog string, args ...string) string {
	t.Helper()
	out, err := exec.Command(prog, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", filepath.Base(prog), strings.Join(args, " "), err, out)
	}
	return string(out)
}

func bigData() []byte {
	b := make([]byte, bigLen)
	for i := range b {
		b[i] = byte(i*7 + i>>10)
	}
	return b
}

// tree writes the source tree mke2fs -d copies.
func tree(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"d/sub", "long", "tmp"} {
		must(os.MkdirAll(filepath.Join(src, d), 0o755))
	}
	must(os.WriteFile(filepath.Join(src, "hello.txt"), []byte(hello), 0o640))
	must(os.Link(filepath.Join(src, "hello.txt"), filepath.Join(src, "d/hard.txt")))
	must(os.Symlink("../hello.txt", filepath.Join(src, "d/rel")))
	must(os.Symlink(slowTarget, filepath.Join(src, "slow")))
	must(os.WriteFile(filepath.Join(src, "d/sub/deep.txt"), []byte("deep\n"), 0o644))
	must(os.WriteFile(filepath.Join(src, "empty"), nil, 0o644))
	must(os.WriteFile(filepath.Join(src, "big.bin"), bigData(), 0o644))
	must(os.WriteFile(filepath.Join(src, "setuid"), []byte("#!/bin/sh\n"), 0o755))
	must(os.Chmod(filepath.Join(src, "setuid"), 0o755|fs.ModeSetuid))
	must(os.Chmod(filepath.Join(src, "tmp"), 0o777|fs.ModeSticky))
	f, err := os.Create(filepath.Join(src, "sparse"))
	must(err)
	_, err = f.WriteAt([]byte("MID"), midOff)
	must(err)
	_, err = f.WriteAt([]byte("END"), sparseLen-3)
	must(err)
	must(f.Close())
	for i := 1; i <= longFiles; i++ {
		must(os.WriteFile(filepath.Join(src, "long", name(i)), []byte("x"), 0o644))
	}
	return src
}

func name(i int) string { return fmt.Sprintf("file-with-a-longish-name-%03d", i) }

// image makes an ext2 volume of src with mke2fs (extra options appended),
// gives hello.txt an owner nobody on the build machine has, and indexes the
// long directory with e2fsck -D when the volume has dir_index.
func image(t *testing.T, src string, opts ...string) string {
	t.Helper()
	mke2fs, debugfs, e2fsck := tool(t, "mke2fs"), tool(t, "debugfs"), tool(t, "e2fsck")
	img := filepath.Join(t.TempDir(), "ext2.img")
	ext := "root_owner=0:0" // mke2fs keeps only its last -E: merge them
	args := []string{"-q", "-F", "-t", "ext2", "-L", "BURROWTEST"}
	for i := 0; i < len(opts); i++ {
		if opts[i] == "-E" && i+1 < len(opts) {
			ext += "," + opts[i+1]
			i++
			continue
		}
		args = append(args, opts[i])
	}
	run(t, mke2fs, append(args, "-E", ext, "-d", src, img, "16M")...)
	run(t, debugfs, "-w", "-R", "sif /hello.txt uid 1234", img)
	run(t, debugfs, "-w", "-R", "sif /hello.txt gid 77", img)
	exec.Command(e2fsck, "-fyD", img).Run() // exit 1: it optimised directories
	run(t, e2fsck, "-fn", img)
	return img
}

// counter counts the reads that reach the disk.
type counter struct {
	r     io.ReaderAt
	reads atomic.Int64
}

func (c *counter) ReadAt(p []byte, off int64) (int, error) {
	c.reads.Add(1)
	return c.r.ReadAt(p, off)
}

func mount(t *testing.T, img string) (*ext2.FS, *counter) {
	t.Helper()
	f, err := os.Open(img)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	c := &counter{r: f}
	fsys, err := ext2.Mount(c)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	return fsys, c
}

func TestMke2fsVolumes(t *testing.T) {
	src := tree(t)
	for _, tc := range []struct {
		name string
		opts []string
	}{
		{"1KiB blocks", []string{"-b", "1024"}},
		{"4KiB blocks", []string{"-b", "4096"}},
		{"revision 0", []string{"-b", "1024", "-E", "revision=0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys, _ := mount(t, image(t, src, tc.opts...))
			if fsys.Label != "BURROWTEST" {
				t.Errorf("label %q", fsys.Label)
			}
			expect := []string{"hello.txt", "d/hard.txt", "d/rel", "slow", "d/sub/deep.txt", "empty", "big.bin", "sparse", "setuid", "tmp", "long/" + name(longFiles)}
			if err := fstest.TestFS(fsys, expect...); err != nil {
				t.Fatal(err)
			}
			checkContents(t, fsys)
			checkLinks(t, fsys)
			checkAttrs(t, fsys)
			ents, err := fs.ReadDir(fsys, "long")
			if err != nil || len(ents) != longFiles {
				t.Errorf("long: %d entries, %v", len(ents), err)
			}
		})
	}
}

func checkContents(t *testing.T, fsys fs.FS) {
	t.Helper()
	for _, p := range []string{"hello.txt", "d/hard.txt", "d/rel", "slow"} {
		if b, err := fs.ReadFile(fsys, p); err != nil || string(b) != hello {
			t.Errorf("%s = %q, %v", p, b, err)
		}
	}
	if b, err := fs.ReadFile(fsys, "big.bin"); err != nil || !bytes.Equal(b, bigData()) {
		t.Errorf("big.bin: %d bytes, %v", len(b), err)
	}
	f, err := fsys.Open("sparse")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ra := f.(io.ReaderAt)
	for _, c := range []struct {
		off  int64
		want string
	}{{midOff, "MID"}, {sparseLen - 3, "END"}, {midOff + 4096, "\x00\x00\x00"}, {0, "\x00\x00\x00"}} {
		b := make([]byte, 3)
		if _, err := ra.ReadAt(b, c.off); err != nil && err != io.EOF || string(b) != c.want {
			t.Errorf("sparse at %d = %q, %v; want %q", c.off, b, err, c.want)
		}
	}
	if info, _ := f.Stat(); info.Size() != sparseLen {
		t.Errorf("sparse size %d", info.Size())
	}
}

func checkLinks(t *testing.T, fsys *ext2.FS) {
	t.Helper()
	for p, want := range map[string]string{"d/rel": "../hello.txt", "slow": slowTarget} {
		if got, err := fsys.ReadLink(p); err != nil || got != want {
			t.Errorf("ReadLink(%s) = %q, %v", p, got, err)
		}
		if info, err := fsys.Lstat(p); err != nil || info.Mode()&fs.ModeSymlink == 0 || info.Size() != int64(len(want)) {
			t.Errorf("Lstat(%s) = %v, %v", p, info, err)
		}
		if info, err := fs.Stat(fsys, p); err != nil || !info.Mode().IsRegular() {
			t.Errorf("Stat(%s) follows to %v, %v", p, info, err)
		}
	}
	if _, err := fsys.ReadLink("hello.txt"); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("ReadLink of a file: %v", err)
	}
	if _, err := fsys.ReadLink("nothere"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReadLink of nothing: %v", err)
	}
}

func attr(t *testing.T, fsys *ext2.FS, p string) (fs.FileInfo, vfs.Attr) {
	t.Helper()
	info, err := fsys.Lstat(p)
	if err != nil {
		t.Fatalf("Lstat(%s): %v", p, err)
	}
	a, ok := info.Sys().(vfs.Attr)
	if !ok {
		t.Fatalf("%s: Sys() is %T, want vfs.Attr", p, info.Sys())
	}
	return info, a
}

func checkAttrs(t *testing.T, fsys *ext2.FS) {
	t.Helper()
	info, a := attr(t, fsys, "hello.txt")
	if a.UID != 1234 || a.GID != 77 || a.Nlink != 2 || info.Mode().Perm() != 0o640 {
		t.Errorf("hello.txt: %+v, mode %v", a, info.Mode())
	}
	if _, h := attr(t, fsys, "d/hard.txt"); h.Ino != a.Ino {
		t.Errorf("hard link: inode %d, hello.txt %d", h.Ino, a.Ino)
	}
	if _, r := attr(t, fsys, "."); r.Ino != 2 || r.UID != 0 {
		t.Errorf("root: %+v (want inode 2, owner root)", r)
	}
	if info, _ := attr(t, fsys, "setuid"); info.Mode()&fs.ModeSetuid == 0 || info.Mode().Perm() != 0o755 {
		t.Errorf("setuid: mode %v", info.Mode())
	}
	if info, _ := attr(t, fsys, "tmp"); info.Mode()&fs.ModeSticky == 0 || !info.IsDir() || info.Mode().Perm() != 0o777 {
		t.Errorf("tmp: mode %v", info.Mode())
	}
	if _, d := attr(t, fsys, "d"); d.Nlink != 3 { // its entry, its ".", sub's ".."
		t.Errorf("d: %d links", d.Nlink)
	}
}

// TestLongDirectoryIsIndexed makes sure the volume above really has an
// htree directory, which the reader must read as a linear one.
func TestLongDirectoryIsIndexed(t *testing.T) {
	img := image(t, tree(t), "-b", "1024")
	if out := run(t, tool(t, "debugfs"), "-R", "stat /long", img); !strings.Contains(out, "Flags: 0x1000") {
		t.Fatalf("/long is not indexed:\n%s", out)
	}
	fsys, _ := mount(t, img)
	if b, err := fs.ReadFile(fsys, "long/"+name(200)); err != nil || string(b) != "x" {
		t.Errorf("a file in the indexed directory: %q, %v", b, err)
	}
}

// TestContiguousBlocksReadAtOnce: a file written in one go is read in a
// handful of disk reads, not one per block (the lesson of W15-S05).
func TestContiguousBlocksReadAtOnce(t *testing.T) {
	fsys, c := mount(t, image(t, tree(t), "-b", "1024"))
	f, err := fsys.Open("big.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b := make([]byte, bigLen)
	before := c.reads.Load()
	if _, err := f.(io.ReaderAt).ReadAt(b, 0); err != nil || !bytes.Equal(b, bigData()) {
		t.Fatalf("ReadAt: %v", err)
	}
	if n := c.reads.Load() - before; n > 24 {
		t.Errorf("%d disk reads for %d blocks", n, bigLen/1024)
	}
}

// TestLinksOutsideTheTree: absolute links resolve from the volume's root, a
// loop is an error, not a hang.
func TestLinksOutsideTheTree(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "f"), []byte(hello), 0o644)
	os.Symlink("/f", filepath.Join(src, "abs"))
	os.Symlink("b", filepath.Join(src, "a"))
	os.Symlink("a", filepath.Join(src, "b"))
	os.Symlink("../../../f", filepath.Join(src, "up"))
	os.MkdirAll(filepath.Join(src, "dir"), 0o755)
	os.Symlink("dir", filepath.Join(src, "dirlink"))
	os.WriteFile(filepath.Join(src, "dir/in"), []byte("in\n"), 0o644)
	fsys, _ := mount(t, image(t, src, "-b", "1024"))
	for _, p := range []string{"abs", "up"} {
		if b, err := fs.ReadFile(fsys, p); err != nil || string(b) != hello {
			t.Errorf("%s = %q, %v", p, b, err)
		}
	}
	if b, err := fs.ReadFile(fsys, "dirlink/in"); err != nil || string(b) != "in\n" {
		t.Errorf("through a directory link: %q, %v", b, err)
	}
	if _, err := fsys.Open("a"); !errors.Is(err, syscall.ELOOP) {
		t.Errorf("a loop opened: %v", err)
	}
}

func TestMountRejects(t *testing.T) {
	img := image(t, tree(t), "-b", "1024")
	good, err := os.ReadFile(img)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		patch func(b []byte)
		want  string
	}{
		{"no magic", func(b []byte) { b[1080] = 0 }, "not ext2"},
		{"extents", func(b []byte) { b[1024+96] |= 0x40 }, "incompatible"},
		{"huge blocks", func(b []byte) { b[1024+24] = 9 }, "block size"},
		{"no inodes", func(b []byte) { copy(b[1024+40:1024+44], []byte{0, 0, 0, 0}) }, "implausible"},
	} {
		b := bytes.Clone(good)
		tc.patch(b)
		if _, err := ext2.Mount(bytes.NewReader(b)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Mount = %v, want %q", tc.name, err, tc.want)
		}
	}
	b := bytes.Clone(good)
	b[1024+100] |= 0x80 // an unknown read-only-compatible feature: readable
	fsys, err := ext2.Mount(bytes.NewReader(b))
	if err != nil || !fsys.ReadOnlyFeatures {
		t.Errorf("unknown ro_compat feature: %v, ReadOnlyFeatures %v", err, fsys != nil && fsys.ReadOnlyFeatures)
	}
}

func TestProbe(t *testing.T) {
	img := image(t, tree(t), "-b", "1024")
	f, _ := os.Open(img)
	defer f.Close()
	if !ext2.Probe(f) {
		t.Error("Probe: an ext2 volume not recognised")
	}
	if ext2.Probe(bytes.NewReader(make([]byte, 4096))) {
		t.Error("Probe: zeros taken for ext2")
	}
}
