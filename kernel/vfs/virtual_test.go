package vfs

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
)

// TestBUG0030MountBelowMissingDir: an ext2 root has no /opt, and the net
// disk is mounted at /opt/net. opt must still be a directory the walker
// passes and listings show, and /opt/net/bin/prog must be reachable.
func TestBUG0030MountBelowMissingDir(t *testing.T) {
	v := New()
	v.Mount(".", fstest.MapFS{"etc/x": {Data: []byte("x")}}, "ext2")
	v.Mount("opt/net", fstest.MapFS{"bin/prog": {Data: []byte("p")}}, "fat32")
	v.Mount("deep/a/b", fstest.MapFS{"f": {Data: []byte("f")}}, "ramfs")
	if got, err := v.Resolve("/opt/net/bin/prog", true, nil); err != nil || got != "opt/net/bin/prog" {
		t.Fatalf("resolve = %q, %v", got, err)
	}
	if info, err := v.Lstat("opt"); err != nil || !info.IsDir() {
		t.Fatalf("lstat opt = %v, %v", info, err)
	}
	names := func(dir string) string {
		es, err := fs.ReadDir(v, dir)
		if err != nil {
			t.Fatalf("readdir %s: %v", dir, err)
		}
		s := ""
		for _, e := range es {
			s += e.Name() + " "
		}
		return s
	}
	if got := names("."); got != "deep etc opt " {
		t.Errorf("/ lists %q", got)
	}
	if got := names("opt"); got != "net " {
		t.Errorf("/opt lists %q", got)
	}
	if got := names("deep/a"); got != "b " {
		t.Errorf("/deep/a lists %q", got)
	}
	if _, err := v.Open("nothere"); err == nil {
		t.Error("a directory no mount lies below was invented")
	}
	if err := fstest.TestFS(v, "etc/x", "opt/net/bin/prog", "deep/a/b/f"); err != nil {
		t.Fatal(err)
	}
}

// TestBUG0032ReadLinkOfDirOnlyMountsMake: readlink of /opt there is EINVAL
// (a directory, not a link), as for any directory. It was ENOENT, from the
// root file system that has no opt, so musl's realpath, which reads each
// component's link, failed for every path under /opt/net (NetSurf found
// none of its resources on an ext2 root).
func TestBUG0032ReadLinkOfDirOnlyMountsMake(t *testing.T) {
	v := New()
	v.Mount(".", fstest.MapFS{"etc/x": {Data: []byte("x")}}, "ext2")
	v.Mount("opt/net", fstest.MapFS{"bin/prog": {Data: []byte("p")}}, "fat32")
	for _, name := range []string{"opt", "opt/net", "etc"} {
		if _, err := v.ReadLink(name); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("readlink %s = %v, want ErrInvalid (not a link)", name, err)
		}
	}
	if _, err := v.ReadLink("nothere"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("readlink nothere = %v, want ErrNotExist", err)
	}
}
