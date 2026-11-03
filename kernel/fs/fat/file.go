package fat

import (
	"io"
	"io/fs"
	"time"
)

type fileInfo struct{ e *entry }

func (i fileInfo) Name() string       { return i.e.name }
func (i fileInfo) Size() int64        { return i.e.size }
func (i fileInfo) ModTime() time.Time { return i.e.modTime }
func (i fileInfo) IsDir() bool        { return i.e.dir }
func (i fileInfo) Sys() any           { return nil }
func (i fileInfo) Mode() fs.FileMode {
	if i.e.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}

// file reads through the cluster chain it resolved at Open.
type file struct {
	fsys     *FS
	e        *entry
	clusters []uint32
	off      int64
}

func (f *file) Stat() (fs.FileInfo, error) { return f.e.info(), nil }
func (f *file) Close() error               { return nil }

func (f *file) Read(p []byte) (int, error) {
	n, err := f.ReadAt(p, f.off)
	f.off += int64(n)
	return n, err
}

// ReadAt reads a run of clusters that lie in a row on the disk at once
// (W15-S05: one disk read per 512-byte cluster made a 15 MB program take
// 11 s to load), so a file written in one go costs one read.
func (f *file) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, &fs.PathError{Op: "read", Path: f.e.name, Err: fs.ErrInvalid}
	}
	done := 0
	cb := f.fsys.clusterBytes
	for done < len(p) && off < f.e.size {
		i := off / cb
		c := f.clusters[i]
		in := off % cb
		want := min(int64(len(p)-done), f.e.size-off)
		run := int64(1) // clusters from c on that follow each other on the disk
		for run*cb-in < want && i+run < int64(len(f.clusters)) && f.clusters[i+run] == c+uint32(run) {
			run++
		}
		n := min(want, run*cb-in)
		if _, err := f.fsys.r.ReadAt(p[done:done+int(n)], f.fsys.dataStart+int64(c-2)*cb+in); err != nil {
			return done, err
		}
		done += int(n)
		off += n
	}
	if done < len(p) {
		return done, io.EOF
	}
	return done, nil
}

func (f *file) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekCurrent:
		offset += f.off
	case io.SeekEnd:
		offset += f.e.size
	}
	if offset < 0 {
		return 0, &fs.PathError{Op: "seek", Path: f.e.name, Err: fs.ErrInvalid}
	}
	f.off = offset
	return offset, nil
}

type dir struct {
	e    *entry
	ents []*entry
	next int
}

func (d *dir) Stat() (fs.FileInfo, error) { return d.e.info(), nil }
func (d *dir) Close() error               { return nil }
func (d *dir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.e.name, Err: fs.ErrInvalid}
}

// ReadDir follows fs.ReadDirFile, in on-disk order.
func (d *dir) ReadDir(n int) ([]fs.DirEntry, error) {
	left := d.ents[d.next:]
	if n > 0 && len(left) == 0 {
		return nil, io.EOF
	}
	if n <= 0 || n > len(left) {
		n = len(left)
	}
	out := make([]fs.DirEntry, n)
	for i, e := range left[:n] {
		out[i] = fs.FileInfoToDirEntry(e.info())
	}
	d.next += n
	return out, nil
}
