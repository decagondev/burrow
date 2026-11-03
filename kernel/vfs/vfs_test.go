package vfs

import (
	"errors"
	"io/fs"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"burrow/kernel/fs/memfs"
)

var t0 = time.Unix(0, 0)

// tree: a frozen root like the initrd (no tmp/ or dev/ of its own, but an
// etc/ that a mount hides partly), a writable /tmp, and /etc/conf.d mounted
// inside a directory of the root.
func tree(t *testing.T) *VFS {
	t.Helper()
	root := memfs.New()
	root.MkdirAll("etc/conf.d", 0o755, t0)
	root.WriteFile("etc/motd", []byte("hello\n"), 0o644, t0)
	root.WriteFile("etc/conf.d/hidden", []byte("under a mount"), 0o644, t0)
	root.Freeze()
	tmp := memfs.New()
	conf := memfs.New()
	conf.WriteFile("net", []byte("dhcp"), 0o644, t0)

	v := New()
	for _, m := range []struct {
		point string
		fsys  fs.FS
	}{{"tmp", tmp}, {".", root}, {"etc/conf.d", conf}} {
		if err := v.Mount(m.point, m.fsys, "memfs"); err != nil {
			t.Fatal(err)
		}
	}
	return v
}

func TestConformance(t *testing.T) {
	v := tree(t)
	if err := v.WriteFile("tmp/note", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(v, "etc/motd", "etc/conf.d/net", "tmp/note"); err != nil {
		t.Fatal(err)
	}
}

func TestMountsShadowAndList(t *testing.T) {
	v := tree(t)
	if _, err := fs.Stat(v, "etc/conf.d/hidden"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the mount should hide the root's etc/conf.d: %v", err)
	}
	ents, err := fs.ReadDir(v, ".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "etc,tmp" {
		t.Errorf("root lists %v, want [etc tmp]", names)
	}
	if st, _ := fs.Stat(v, "tmp"); st == nil || st.Name() != "tmp" || !st.IsDir() {
		t.Errorf("Stat(tmp) = %v", st)
	}
	got := ""
	for _, m := range v.Mounts() {
		got += m.Point + " "
	}
	if got != "/ /tmp /etc/conf.d " {
		t.Errorf("Mounts = %q", got)
	}
	if err := v.Mount("tmp", memfs.New(), "memfs"); !errors.Is(err, fs.ErrExist) {
		t.Errorf("second mount on tmp = %v", err)
	}
}

func TestWritesGoToTheRightMount(t *testing.T) {
	v := tree(t)
	for _, c := range []struct {
		err  error
		want error
	}{
		{v.WriteFile("etc/new", []byte("x")), fs.ErrPermission}, // the frozen root
		{v.Mkdir("tmp/d"), nil},
		{v.WriteFile("tmp/d/f", []byte("data")), nil},
		{v.Remove("tmp"), fs.ErrPermission}, // a mount point is busy
		{v.Remove("tmp/d"), fs.ErrExist},    // not empty
		{v.WriteFile("../x", nil), fs.ErrInvalid},
	} {
		if !errors.Is(c.err, c.want) && !(c.want == nil && c.err == nil) {
			t.Errorf("got %v, want %v", c.err, c.want)
		}
	}
	if b, err := fs.ReadFile(v, "tmp/d/f"); err != nil || string(b) != "data" {
		t.Errorf("tmp/d/f = %q, %v", b, err)
	}
	var pe *fs.PathError
	if err := v.WriteFile("etc/new", nil); !errors.As(err, &pe) || pe.Path != "etc/new" {
		t.Errorf("error names %v, want the VFS path", err)
	}
}

func TestClean(t *testing.T) {
	for in, want := range map[string]string{"/": ".", "": ".", "/etc/motd": "etc/motd", "etc//x/": "etc/x", "/../a": "a"} {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

// Rename stays inside one mount (EXDEV otherwise, as Linux) and never
// moves a mount point.
func TestRenameStaysInsideAMount(t *testing.T) {
	v := tree(t)
	v.WriteFile("tmp/a", []byte("a"))
	if err := v.Rename("tmp/a", "tmp/b"); err != nil {
		t.Fatal(err)
	}
	if b, err := fs.ReadFile(v, "tmp/b"); err != nil || string(b) != "a" {
		t.Errorf("tmp/b = %q, %v", b, err)
	}
	for _, c := range []struct {
		from, to string
		want     error
	}{
		{"tmp/b", "etc/b", syscall.EXDEV},
		{"etc/motd", "etc/m2", fs.ErrPermission}, // the frozen root
		{"tmp", "tmp2", fs.ErrPermission},        // a mount point
	} {
		if err := v.Rename(c.from, c.to); !errors.Is(err, c.want) {
			t.Errorf("Rename(%s, %s) = %v, want %v", c.from, c.to, err, c.want)
		}
	}
}
