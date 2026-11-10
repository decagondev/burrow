package vfs

import (
	"io"
	"io/fs"
	"sort"
)

// renamed gives a mounted file system's root the name of its mount point
// (fs.FileInfo.Name of "tmp" must be "tmp", not ".").
type renamed struct {
	fs.File
	name string
}

func (r renamed) Stat() (fs.FileInfo, error) {
	st, err := r.File.Stat()
	if err != nil {
		return nil, err
	}
	return namedInfo{st, r.name}, nil
}

func (r renamed) ReadDir(n int) ([]fs.DirEntry, error) {
	if d, ok := r.File.(fs.ReadDirFile); ok {
		return d.ReadDir(n)
	}
	return nil, &fs.PathError{Op: "readdir", Path: r.name, Err: fs.ErrInvalid}
}

type namedInfo struct {
	fs.FileInfo
	name string
}

func (i namedInfo) Name() string { return i.name }

// mergedDir lists a directory's own entries plus what the mounts below it
// add (a mount point hides an entry of the same name; virtual.go), in name
// order.
type mergedDir struct {
	fs.File
	entries []fs.DirEntry
	next    int
}

func newMergedDir(f fs.File, extra map[string]fs.FileInfo) (fs.File, error) {
	var own []fs.DirEntry
	if d, ok := f.(fs.ReadDirFile); ok {
		var err error
		if own, err = d.ReadDir(-1); err != nil {
			f.Close()
			return nil, err
		}
	}
	byName := map[string]fs.DirEntry{}
	for _, e := range own {
		byName[e.Name()] = e
	}
	for name, info := range extra {
		if _, way := info.(dirInfo); way && byName[name] != nil {
			continue // a real directory on the way to a deeper mount stays itself
		}
		byName[name] = fs.FileInfoToDirEntry(info)
	}
	entries := make([]fs.DirEntry, 0, len(byName))
	for _, e := range byName {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return &mergedDir{File: f, entries: entries}, nil
}

// ReadDir follows fs.ReadDirFile.
func (d *mergedDir) ReadDir(n int) ([]fs.DirEntry, error) {
	left := d.entries[d.next:]
	if n <= 0 {
		d.next = len(d.entries)
		return left, nil
	}
	if len(left) == 0 {
		return nil, io.EOF
	}
	n = min(n, len(left))
	d.next += n
	return left[:n], nil
}
