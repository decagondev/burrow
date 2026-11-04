package ext2_test

// helpers_test.go - volumes for the tests (W19-S01, S02): e2fsprogs found
// (in /sbin, often off PATH), source trees, mke2fs images with owners set
// by debugfs and directories indexed by e2fsck -D, and mounts, read-only
// and writable. Skips without e2fsprogs unless BURROW_EXT2_REQUIRED is set.

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"burrow/kernel/fs/ext2"
)

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

// fileDisk is an image file as the writer's Disk.
type fileDisk struct{ *os.File }

func (fileDisk) Flush() error { return nil }

// blank makes an empty volume: 8 MiB of 1 KiB blocks in groups of 2048
// blocks, so allocation crosses groups, plus a directory
// with an htree index to change.
func blank(t *testing.T, opts ...string) string {
	t.Helper()
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "indexed"), 0o755)
	for i := 1; i <= 300; i++ {
		os.WriteFile(filepath.Join(src, "indexed", name(i)), []byte("x"), 0o644)
	}
	mke2fs, e2fsck := tool(t, "mke2fs"), tool(t, "e2fsck")
	img := filepath.Join(t.TempDir(), "w.img")
	args := append([]string{"-q", "-F", "-t", "ext2", "-L", "WRITE", "-E", "root_owner=0:0", "-b", "1024", "-g", "2048", "-N", "2048"}, opts...)
	run(t, mke2fs, append(args, "-d", src, img, "8M")...)
	exec.Command(e2fsck, "-fyD", img).Run()
	run(t, e2fsck, "-fn", img)
	return img
}

func mountRW(t *testing.T, img string) (*ext2.FS, func()) {
	t.Helper()
	f, err := os.OpenFile(img, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	fsys, err := ext2.MountRW(fileDisk{f})
	if err != nil {
		f.Close()
		t.Fatalf("MountRW: %v", err)
	}
	return fsys, func() { f.Close() }
}

func fsck(t *testing.T, img, what string) {
	t.Helper()
	if out, err := exec.Command(tool(t, "e2fsck"), "-fn", img).CombinedOutput(); err != nil {
		t.Fatalf("%s: e2fsck -fn rejects the volume: %v\n%s", what, err, out)
	}
}
