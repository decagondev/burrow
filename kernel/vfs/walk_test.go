package vfs

// walk_test.go - the path walker (W19-S05): links followed or not, ".."
// after a link, a link into another mount and one back, absolute targets,
// loops, a trailing slash, the visit callback, and Link across mounts.

import (
	"errors"
	"io/fs"
	"syscall"
	"testing"
	"time"

	"burrow/kernel/fs/memfs"
	"burrow/kernel/fsattr"
)

func linkTree(t *testing.T) *VFS {
	t.Helper()
	root, tmp := memfs.New(), memfs.New()
	n := fsattr.New{Mtime: time.Unix(0, 0)}
	root.MkdirAll("a/b", 0o755, n.Mtime)
	root.WriteFile("a/x", []byte("x in a"), 0o644, n.Mtime)
	root.WriteFile("a/b/f", []byte("f in b"), 0o644, n.Mtime)
	tmp.WriteFile("t", []byte("in tmp"), 0o644, n.Mtime)
	for _, l := range [][2]string{{"a/b", "ab"}, {"/tmp", "totmp"}, {"loop2", "loop1"}, {"loop1", "loop2"}, {"nowhere", "dang"}} {
		if err := root.Symlink(l[0], l[1], n); err != nil {
			t.Fatal(err)
		}
	}
	tmp.Symlink("../a/x", "back", n) // from /tmp, up out of the mount
	v := New()
	v.Mount(".", root, "memfs")
	v.Mount("tmp", tmp, "ramfs")
	return v
}

func TestResolve(t *testing.T) {
	v := linkTree(t)
	for _, c := range []struct {
		raw    string
		follow bool
		want   string
		err    error
	}{
		{"ab/f", true, "a/b/f", nil},
		{"ab/../x", true, "a/x", nil}, // the text says "x"
		{"ab", false, "ab", nil},
		{"ab", true, "a/b", nil},
		{"ab/", false, "a/b", nil}, // a trailing slash follows
		{"totmp/t", true, "tmp/t", nil},
		{"tmp/back", true, "a/x", nil},
		{"/a//./b/../x", true, "a/x", nil},
		{"../../a", true, "a", nil},           // ".." stops at the root
		{"a/newfile", true, "a/newfile", nil}, // a missing last part is for the caller
		{"dang", true, "nowhere", nil},
		{"nothere/f", true, "", fs.ErrNotExist},
		{"a/x/y", true, "", syscall.ENOTDIR},
		{"a/x/", true, "", syscall.ENOTDIR},
		{"loop1", true, "", syscall.ELOOP},
		{"loop1", false, "loop1", nil},
	} {
		got, err := v.Resolve(c.raw, c.follow, nil)
		if got != c.want || !errors.Is(err, c.err) && (c.err != nil || err != nil) {
			t.Errorf("Resolve(%q, %v) = %q, %v; want %q, %v", c.raw, c.follow, got, err, c.want, c.err)
		}
	}
}

func TestResolveVisits(t *testing.T) {
	v := linkTree(t)
	var seen []string
	if _, err := v.Resolve("ab/f", true, func(dir string, _ fs.FileInfo) error {
		seen = append(seen, dir)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{".", ".", "a", "a/b"}; len(seen) != len(want) || seen[1] != "." || seen[3] != "a/b" {
		t.Errorf("visited %v, want %v (the root again after the link)", seen, want)
	}
	refuse := errors.New("no")
	if _, err := v.Resolve("a/b/f", true, func(dir string, _ fs.FileInfo) error {
		if dir == "a" {
			return refuse
		}
		return nil
	}); !errors.Is(err, refuse) {
		t.Errorf("a refusal: %v", err)
	}
}

func TestLinkAndLstat(t *testing.T) {
	v := linkTree(t)
	if info, err := v.Lstat("ab"); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("Lstat(ab) = %v, %v", info, err)
	}
	if target, err := v.ReadLink("tmp/back"); err != nil || target != "../a/x" {
		t.Errorf("ReadLink(tmp/back) = %q, %v", target, err)
	}
	if _, err := v.ReadLink("a/x"); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("ReadLink of a file: %v", err)
	}
	if info, err := v.Lstat("tmp"); err != nil || !info.IsDir() {
		t.Errorf("Lstat of a mount point: %v, %v", info, err)
	}
	if err := v.Link("a/x", "tmp/x"); !errors.Is(err, syscall.EXDEV) {
		t.Errorf("a hard link across mounts: %v", err)
	}
	if err := v.Link("tmp/t", "tmp/t2"); err != nil {
		t.Errorf("a hard link in /tmp: %v", err)
	}
}
