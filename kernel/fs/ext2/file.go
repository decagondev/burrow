package ext2

// file.go - open files and directories (W19-S01): a file reads through its
// block map (io.ReaderAt, which lazy execve wants, W18-S02) and seeks; a
// directory lists its entries in on-disk order (fs.ReadDirFile). Never
// writes. Docs: WAVE-19-unix.md.

import (
	"io"
	"io/fs"
)

type file struct {
	fsys *FS
	in   *inode
	name string
	m    *blockMap
	off  int64
}

func (f *file) Stat() (fs.FileInfo, error) { return f.fsys.info(f.name, f.in), nil }
func (f *file) Close() error               { return nil }

func (f *file) Read(p []byte) (int, error) {
	n, err := f.ReadAt(p, f.off)
	f.off += int64(n)
	if err == io.EOF && n > 0 {
		err = nil
	}
	return n, err
}

func (f *file) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, &fs.PathError{Op: "read", Path: f.name, Err: fs.ErrInvalid}
	}
	n, err := f.m.readAt(p, off)
	if err != nil && err != io.EOF {
		return n, &fs.PathError{Op: "read", Path: f.name, Err: err}
	}
	return n, err
}

func (f *file) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekCurrent:
		offset += f.off
	case io.SeekEnd:
		offset += f.in.size
	}
	if offset < 0 {
		return 0, &fs.PathError{Op: "seek", Path: f.name, Err: fs.ErrInvalid}
	}
	f.off = offset
	return offset, nil
}

type dir struct {
	fsys *FS
	info fileInfo
	ents []dirent
	next int
}

func (d *dir) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *dir) Close() error               { return nil }
func (d *dir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.info.name, Err: fs.ErrInvalid}
}

// ReadDir follows fs.ReadDirFile.
func (d *dir) ReadDir(n int) ([]fs.DirEntry, error) {
	left := d.ents[d.next:]
	if n > 0 && len(left) == 0 {
		return nil, io.EOF
	}
	if n <= 0 || n > len(left) {
		n = len(left)
	}
	out := make([]fs.DirEntry, 0, n)
	for _, e := range left[:n] {
		de, err := d.fsys.entry(e)
		if err != nil {
			return out, &fs.PathError{Op: "readdir", Path: d.info.name, Err: err}
		}
		out = append(out, de)
	}
	d.next += n
	return out, nil
}
