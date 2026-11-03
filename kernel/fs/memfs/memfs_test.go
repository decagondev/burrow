package memfs

import (
	"errors"
	"io/fs"
	"syscall"
	"testing"
	"testing/fstest"
	"time"
)

var t0 = time.Unix(1700000000, 0)

func sample(t *testing.T) *FS {
	t.Helper()
	f := New()
	for _, err := range []error{
		f.MkdirAll("etc/ssh", 0o755, t0),
		f.WriteFile("etc/motd", []byte("hello\n"), 0o644, t0),
		f.WriteFile("etc/ssh/key", []byte("k"), 0o600, t0),
		f.WriteFile("empty", nil, 0o644, t0),
		f.Mkdir("tmp", 0o777, t0),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// The standard library's conformance suite: Open, Stat, ReadDir in every
// chunk size, Seek, ReadAt, fs.WalkDir, Glob...
func TestConformance(t *testing.T) {
	if err := fstest.TestFS(sample(t), "etc/motd", "etc/ssh/key", "empty", "tmp"); err != nil {
		t.Fatal(err)
	}
}

func TestWritesReplaceAndRemove(t *testing.T) {
	f := sample(t)
	if err := f.WriteFile("etc/motd", []byte("v2"), 0o644, t0); err != nil {
		t.Fatal(err)
	}
	if b, _ := fs.ReadFile(f, "etc/motd"); string(b) != "v2" {
		t.Errorf("motd = %q after rewrite", b)
	}
	for _, c := range []struct {
		err  error
		want error
	}{
		{f.WriteFile("nodir/x", nil, 0o644, t0), fs.ErrNotExist},
		{f.WriteFile("etc", nil, 0o644, t0), fs.ErrExist},
		{f.Mkdir("etc", 0o755, t0), fs.ErrExist},
		{f.Remove("etc"), fs.ErrExist}, // not empty
		{f.Remove("nope"), fs.ErrNotExist},
		{f.WriteFile("../x", nil, 0o644, t0), fs.ErrInvalid},
	} {
		if !errors.Is(c.err, c.want) {
			t.Errorf("got %v, want %v", c.err, c.want)
		}
	}
	if err := f.Remove("etc/ssh/key"); err != nil {
		t.Fatal(err)
	}
	if err := f.Remove("etc/ssh"); err != nil {
		t.Fatal(err)
	}
	if files, dirs, size := f.Counts(); files != 2 || dirs != 3 || size != 2 {
		t.Errorf("Counts = %d files, %d dirs, %d bytes; want 2, 3, 2", files, dirs, size)
	}
}

func TestFreeze(t *testing.T) {
	f := sample(t)
	f.Freeze()
	if err := f.WriteFile("x", nil, 0o644, t0); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("WriteFile after Freeze = %v", err)
	}
	if err := f.Remove("empty"); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("Remove after Freeze = %v", err)
	}
	if b, err := fs.ReadFile(f, "etc/motd"); err != nil || string(b) != "hello\n" {
		t.Errorf("read after Freeze = %q, %v", b, err)
	}
}

// Rename follows rename(2): it replaces a file, or an empty directory with
// a directory, and refuses the mismatched and impossible cases.
func TestRename(t *testing.T) {
	f := New()
	f.MkdirAll("a/b", 0o755, t0)
	f.MkdirAll("full/x", 0o755, t0)
	f.MkdirAll("empty", 0o755, t0)
	f.WriteFile("a/one", []byte("1"), 0o644, t0)
	f.WriteFile("two", []byte("2"), 0o644, t0)
	for _, c := range []struct {
		from, to string
		want     error
	}{
		{"a/one", "a/b/moved", nil},     // into another directory
		{"two", "a/b/moved", nil},       // replaces a file
		{"a/b/moved", "a/b/moved", nil}, // to itself: nothing happens
		{"a", "a/b/inside", syscall.EINVAL},
		{"a/b/moved", "empty", syscall.EISDIR},
		{"full/x", "a/b/moved", syscall.ENOTDIR},
		{"a/b", "full", syscall.ENOTEMPTY},
		{"nope", "x", fs.ErrNotExist},
		{"a/b", "empty", nil}, // a directory replaces an empty one
	} {
		if err := f.Rename(c.from, c.to); !errors.Is(err, c.want) && (c.want != nil || err != nil) {
			t.Errorf("Rename(%s, %s) = %v, want %v", c.from, c.to, err, c.want)
		}
	}
	if b, err := fs.ReadFile(f, "empty/moved"); err != nil || string(b) != "2" {
		t.Errorf("empty/moved = %q, %v", b, err)
	}
	if _, err := fs.Stat(f, "a/b"); !errors.Is(err, fs.ErrNotExist) {
		t.Error("a/b still there after it moved")
	}
}
