package procfs

// file.go - open files and directories: a file's bytes are generated once
// at open (a reader that reads in small pieces sees one snapshot) and it
// reports size 0, as Linux's proc files do; a directory lists the names its
// node gives, skipping any that vanished in between. Docs: WAVE-23-proc-virtio.md.

import (
	"bytes"
	"io"
	"io/fs"
	"path"
	"time"
)

// info is a node's fs.FileInfo.
type info struct {
	f *FS
	n node
}

func (i info) Name() string       { return path.Base(i.n.name) }
func (i info) Size() int64        { return 0 }
func (i info) Mode() fs.FileMode  { return i.n.mode }
func (i info) ModTime() time.Time { return i.f.st.Boot }
func (i info) IsDir() bool        { return i.n.mode.IsDir() }
func (i info) Sys() any           { return i.n.attr }

// file is an open file: its bytes, read and seeked.
type file struct {
	info
	*bytes.Reader
}

func newFile(f *FS, n node) *file { return &file{info{f, n}, bytes.NewReader(n.data())} }

func (fl *file) Stat() (fs.FileInfo, error) { return fl.info, nil }
func (fl *file) Close() error               { return nil }

// dir is an open directory.
type dir struct {
	info
	path    string
	entries []fs.DirEntry
	read    bool
}

func (f *FS) openDir(n node, name string) *dir { return &dir{info: info{f, n}, path: name} }

func (d *dir) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *dir) Close() error               { return nil }
func (d *dir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.path, Err: fs.ErrInvalid}
}

// ReadDir lists the directory as fs.ReadDirFile says: n > 0 at most n
// entries per call (io.EOF at the end), n <= 0 the rest.
func (d *dir) ReadDir(n int) ([]fs.DirEntry, error) {
	if !d.read {
		d.read = true
		for _, name := range sorted(d.n.list()) {
			sub := name
			if d.path != "." {
				sub = d.path + "/" + name
			}
			if c, err := d.f.lookup(sub); err == nil {
				d.entries = append(d.entries, fs.FileInfoToDirEntry(info{d.f, c}))
			}
		}
	}
	if n <= 0 {
		out := d.entries
		d.entries = nil
		return out, nil
	}
	if len(d.entries) == 0 {
		return nil, io.EOF
	}
	k := min(n, len(d.entries))
	out := d.entries[:k]
	d.entries = d.entries[k:]
	return out, nil
}
