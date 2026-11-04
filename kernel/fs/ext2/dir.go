package ext2

// dir.go - directories (W19-S01): a directory's data is a list of entries
// {inode, record length, name length, type, name}; an entry with inode 0 is
// unused space. An htree-indexed directory (dir_index) is read the same
// way: its index blocks look like empty entries to a linear reader, which
// is how the format was designed. Never trusts a record length: each must
// stay inside its block. Docs: WAVE-19-unix.md.

import (
	"fmt"
	"io"
	"io/fs"
)

// dirent is one entry; typ is the filetype byte (0 when the volume has none).
type dirent struct {
	name string
	ino  uint32
	typ  uint8
}

// filetype values in directory entries
const (
	ftReg  = 1
	ftDir  = 2
	ftChr  = 3
	ftBlk  = 4
	ftFifo = 5
	ftSock = 6
	ftLink = 7
)

// readDir lists a directory's entries, "." and ".." left out.
func (f *FS) readDir(in *inode) ([]dirent, error) {
	data := make([]byte, in.size)
	if _, err := newBlockMap(f, in).readAt(data, 0); err != nil && err != io.EOF {
		return nil, err
	}
	var out []dirent
	bs := int(f.sb.blockSize)
	for blk := 0; blk < len(data); blk += bs {
		b := data[blk:min(blk+bs, len(data))]
		for off := 0; off < len(b); {
			if off+deName > len(b) {
				return nil, fmt.Errorf("inode %d: a directory entry crosses its block", in.ino)
			}
			rec := int(le.Uint16(b[off+deRecLen:]))
			nameLen := int(le.Uint16(b[off+deNameLen:]))
			typ := uint8(0)
			if f.hasType {
				nameLen, typ = int(b[off+deNameLen]), b[off+deFileType]
			}
			if rec < deName || rec%4 != 0 || off+rec > len(b) || deName+nameLen > rec {
				return nil, fmt.Errorf("inode %d: a broken directory entry at %d", in.ino, blk+off)
			}
			ino := le.Uint32(b[off+deInode:])
			name := string(b[off+deName : off+deName+nameLen])
			if ino != 0 && name != "." && name != ".." {
				out = append(out, dirent{name: name, ino: ino, typ: typ})
			}
			off += rec
		}
	}
	return out, nil
}

// child is the inode named name in directory in.
func (f *FS) child(in *inode, name string) (*inode, error) {
	ents, err := f.readDir(in)
	if err != nil {
		return nil, err
	}
	for _, e := range ents {
		if e.name == name {
			return f.inode(e.ino)
		}
	}
	return nil, fs.ErrNotExist
}

// typeOf is an entry's type as fs.FileMode bits, from the filetype byte.
func typeOf(typ uint8) (fs.FileMode, bool) {
	switch typ {
	case ftReg:
		return 0, true
	case ftDir:
		return fs.ModeDir, true
	case ftLink:
		return fs.ModeSymlink, true
	case ftChr:
		return fs.ModeDevice | fs.ModeCharDevice, true
	case ftBlk:
		return fs.ModeDevice, true
	case ftFifo:
		return fs.ModeNamedPipe, true
	case ftSock:
		return fs.ModeSocket, true
	}
	return 0, false
}

// dirEntry is an fs.DirEntry that reads its inode only when Info asks
// (listing a directory then costs no inode reads, unless the volume has no
// filetype feature and the type must come from the inode).
type dirEntry struct {
	fsys *FS
	e    dirent
	typ  fs.FileMode
}

func (d dirEntry) Name() string      { return d.e.name }
func (d dirEntry) IsDir() bool       { return d.typ == fs.ModeDir }
func (d dirEntry) Type() fs.FileMode { return d.typ }
func (d dirEntry) String() string    { return fs.FormatDirEntry(d) }

func (d dirEntry) Info() (fs.FileInfo, error) {
	in, err := d.fsys.inode(d.e.ino)
	if err != nil {
		return nil, err
	}
	return d.fsys.info(d.e.name, in), nil
}

func (f *FS) entry(e dirent) (fs.DirEntry, error) {
	if typ, ok := typeOf(e.typ); ok {
		return dirEntry{f, e, typ}, nil
	}
	in, err := f.inode(e.ino)
	if err != nil {
		return nil, err
	}
	return dirEntry{f, e, fileMode(in.mode).Type()}, nil
}
