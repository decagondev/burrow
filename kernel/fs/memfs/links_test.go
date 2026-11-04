package memfs

// links_test.go - links in a tree (W19-S05): fstest over a tree with
// symbolic and hard links, link counts, loops and absolute targets.

import (
	"errors"
	"io/fs"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"burrow/kernel/fsattr"
)

func TestLinks(t *testing.T) {
	f := New()
	n := fsattr.New{Mtime: time.Unix(0, 0)}
	f.MkdirAll("a/b", 0o755, n.Mtime)
	f.WriteFile("a/b/file", []byte("data"), 0o644, n.Mtime)
	for _, l := range [][2]string{{"b/file", "a/rel"}, {"/a/b", "abs"}, {"../b", "a/b/up"}, {"loop2", "loop1"}, {"loop1", "loop2"}} {
		if err := f.Symlink(l[0], l[1], n); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Link("a/b/file", "hard"); err != nil {
		t.Fatal(err)
	}
	f.Remove("loop1") // fstest opens every link; a loop would fail it
	f.Remove("loop2")
	if err := fstest.TestFS(f, "a/rel", "abs", "hard", "a/b/up"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"a/rel", "abs/file", "a/b/up/file", "hard"} {
		if b, err := fs.ReadFile(f, p); err != nil || string(b) != "data" {
			t.Errorf("%s = %q, %v", p, b, err)
		}
	}
	info, _ := f.Lstat("hard")
	if a, _ := fsattr.Of(info); a.Nlink != 2 || info.Name() != "hard" {
		t.Errorf("hard: %d links, named %q", a.Nlink, info.Name())
	}
	f.WriteFile("hard", []byte("new"), 0o644, n.Mtime)
	if b, _ := fs.ReadFile(f, "a/b/file"); string(b) != "new" {
		t.Errorf("a rewrite through one name: the other reads %q", b)
	}
	f.Symlink("l2", "l1", n)
	f.Symlink("l1", "l2", n)
	if _, err := f.Open("l1"); !errors.Is(err, syscall.ELOOP) {
		t.Errorf("a loop: %v", err)
	}
	if err := f.Link("a", "a2"); !errors.Is(err, syscall.EPERM) {
		t.Errorf("a hard link to a directory: %v", err)
	}
	if _, err := f.ReadLink("hard"); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("ReadLink of a file: %v", err)
	}
}
