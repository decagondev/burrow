package tarfs

import (
	"archive/tar"
	"bytes"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"burrow/kernel/fsattr"
)

// archive builds what `tar --format=ustar -C initrd -cf - .` produces.
func archive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	mt := time.Unix(0, 0)
	add := func(h *tar.Header, body string) {
		h.ModTime, h.Format = mt, tar.FormatUSTAR
		h.Size = int64(len(body))
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	add(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o755}, "")
	add(&tar.Header{Name: "./etc/", Typeflag: tar.TypeDir, Mode: 0o755}, "")
	add(&tar.Header{Name: "./etc/motd", Typeflag: tar.TypeReg, Mode: 0o644}, "Welcome to Burrow\n")
	add(&tar.Header{Name: "./bin/sh", Typeflag: tar.TypeSymlink, Linkname: "busybox", Mode: 0o777}, "")
	add(&tar.Header{Name: "usr/share/doc/README", Typeflag: tar.TypeReg, Mode: 0o644}, "made-up dirs\n")
	add(&tar.Header{Name: "./etc/shadow", Typeflag: tar.TypeReg, Mode: 0o600, Uid: 0, Gid: 42}, "root:x\n")
	add(&tar.Header{Name: "./bin/su", Typeflag: tar.TypeReg, Mode: 0o4755}, "su\n")
	add(&tar.Header{Name: "./home/burrow/", Typeflag: tar.TypeDir, Mode: 0o700, Uid: 1000, Gid: 1000}, "")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestConformanceAndContents(t *testing.T) {
	a := archive(t)
	fsys, err := New(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(fsys, "etc/motd", "usr/share/doc/README"); err != nil {
		t.Fatal(err)
	}
	b, _ := fs.ReadFile(fsys, "etc/motd")
	if string(b) != "Welcome to Burrow\n" {
		t.Errorf("motd = %q", b)
	}
	if !bytes.Contains(a, b) || &b[0] == nil {
		t.Error("motd not found in the archive")
	}
	if _, err := fs.Stat(fsys, "bin/sh"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("symlink should be skipped, Stat = %v", err)
	}
	if err := fsys.WriteFile("etc/x", nil, 0o644, time.Time{}); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("tarfs is writable: %v", err)
	}
}

// File data are views into the archive, not copies.
func TestZeroCopy(t *testing.T) {
	a := archive(t)
	fsys, err := New(a)
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(a, []byte("Welcome"))
	a[i] = 'w'
	if b, _ := fs.ReadFile(fsys, "etc/motd"); b[0] != 'w' {
		t.Errorf("motd = %q: tarfs copied the data", b)
	}
}

func TestTruncatedArchive(t *testing.T) {
	a := archive(t)
	if _, err := New(a[:700]); err == nil {
		t.Error("New accepted a truncated archive")
	}
}

// TestOwnersAndModes (W19-S04): headers' owners and modes, set-id bits
// included, come through as fsattr.Attr and fs.FileMode.
func TestOwnersAndModes(t *testing.T) {
	fsys, err := New(archive(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name     string
		mode     fs.FileMode
		uid, gid uint32
	}{
		{"etc/shadow", 0o600, 0, 42},
		{"bin/su", 0o755 | fs.ModeSetuid, 0, 0},
		{"home/burrow", fs.ModeDir | 0o700, 1000, 1000},
		{"usr/share", fs.ModeDir | 0o755, 0, 0}, // made up
	} {
		info, err := fs.Stat(fsys, c.name)
		if err != nil {
			t.Fatal(err)
		}
		a, ok := fsattr.Of(info)
		if !ok || info.Mode() != c.mode || a.UID != c.uid || a.GID != c.gid {
			t.Errorf("%s: mode %v, attr %+v (want %v, %d:%d)", c.name, info.Mode(), a, c.mode, c.uid, c.gid)
		}
	}
}
