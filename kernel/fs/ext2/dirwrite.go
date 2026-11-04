package ext2

// dirwrite.go - changing directories (W19-S02): an entry goes into the
// first gap big enough (an unused entry, or the slack after an entry's
// name), else into a new block at the end; a removed entry's space joins
// the entry before it, or the entry is marked unused when it is the first
// in its block. Any change drops an htree index (EXT2_INDEX_FL), as
// Linux's ext2 does: the index blocks are then read as empty entries. Never
// lets an entry cross its block. Docs: WAVE-19-unix.md.

import (
	"fmt"
	"io/fs"
)

func recLen(nameLen int) int { return (deName + nameLen + 3) &^ 3 }

// nameLenAt is the name length of the entry at b.
func (f *FS) nameLenAt(b []byte) int {
	if f.hasType {
		return int(b[deNameLen])
	}
	return int(le.Uint16(b[deNameLen:]))
}

func (f *FS) putEntry(b []byte, ino uint32, rec int, name string, ft uint8) {
	le.PutUint32(b[deInode:], ino)
	le.PutUint16(b[deRecLen:], uint16(rec))
	if f.hasType {
		b[deNameLen], b[deFileType] = byte(len(name)), ft
	} else {
		le.PutUint16(b[deNameLen:], uint16(len(name)))
	}
	n := copy(b[deName:], name)
	clear(b[deName+n : recLen(len(name))])
}

// ftOf is the directory entry type of an inode.
func ftOf(in *inode) uint8 {
	switch in.mode & modeType {
	case modeDir:
		return ftDir
	case modeLink:
		return ftLink
	case modeChr:
		return ftChr
	case modeBlk:
		return ftBlk
	case modeFifo:
		return ftFifo
	case modeSock:
		return ftSock
	}
	return ftReg
}

// dirBlock reads directory block i: its contents and where it lives.
func (f *FS) dirBlock(dir *inode, i int64) ([]byte, uint32, error) {
	blk, err := newBlockMap(f, dir).lookup(uint64(i))
	if err != nil || blk == 0 {
		return nil, 0, fmt.Errorf("inode %d: directory block %d missing (%v)", dir.ino, i, err)
	}
	b := make([]byte, f.sb.blockSize)
	_, err = f.r.ReadAt(b, int64(blk)*f.sb.blockSize)
	return b, blk, err
}

func (f *FS) putBlock(b []byte, blk uint32) error {
	_, err := f.disk.WriteAt(b, int64(blk)*f.sb.blockSize)
	return err
}

// addEntry adds name -> in to dir and writes dir.
func (f *FS) addEntry(dir *inode, name string, in *inode) error {
	need, bs := recLen(len(name)), f.sb.blockSize
	for i := int64(0); i < dir.size/bs; i++ {
		b, blk, err := f.dirBlock(dir, i)
		if err != nil {
			return err
		}
		for off := 0; off+deName <= len(b); {
			rec := int(le.Uint16(b[off+deRecLen:]))
			if rec < deName || off+rec > len(b) {
				return fmt.Errorf("inode %d: a broken directory entry", dir.ino)
			}
			used := 0
			if le.Uint32(b[off+deInode:]) != 0 {
				used = recLen(f.nameLenAt(b[off:]))
			}
			if rec-used >= need {
				if used > 0 {
					le.PutUint16(b[off+deRecLen:], uint16(used))
				}
				f.putEntry(b[off+used:], in.ino, rec-used, name, ftOf(in))
				if err := f.putBlock(b, blk); err != nil {
					return err
				}
				return f.touchDir(dir)
			}
			off += rec
		}
	}
	return f.growDir(dir, name, in)
}

// growDir adds a block to dir holding one entry.
func (f *FS) growDir(dir *inode, name string, in *inode) error {
	bs := f.sb.blockSize
	saved := dir.ptr
	t := newTree(f, dir, f.goal(dir))
	blk, err := t.place(uint64(dir.size / bs))
	if err == nil {
		err = t.flush()
	}
	if err != nil {
		for _, b := range t.taken {
			f.freeBlock(b)
		}
		dir.ptr = saved
		return err
	}
	b := make([]byte, bs)
	f.putEntry(b, in.ino, int(bs), name, ftOf(in))
	if err := f.putBlock(b, blk); err != nil {
		return err
	}
	dir.size += bs
	dir.blocks512 += uint32(1+t.nTable) * uint32(bs/512)
	return f.touchDir(dir)
}

// removeEntry takes name out of dir and writes dir.
func (f *FS) removeEntry(dir *inode, name string) error {
	bs := f.sb.blockSize
	for i := int64(0); i < dir.size/bs; i++ {
		b, blk, err := f.dirBlock(dir, i)
		if err != nil {
			return err
		}
		prev := -1
		for off := 0; off+deName <= len(b); {
			rec := int(le.Uint16(b[off+deRecLen:]))
			if rec < deName || off+rec > len(b) {
				return fmt.Errorf("inode %d: a broken directory entry", dir.ino)
			}
			nl := f.nameLenAt(b[off:])
			if le.Uint32(b[off+deInode:]) != 0 && deName+nl <= rec && string(b[off+deName:off+deName+nl]) == name {
				if prev >= 0 {
					le.PutUint16(b[prev+deRecLen:], uint16(off+rec-prev))
				} else {
					le.PutUint32(b[off+deInode:], 0)
				}
				if err := f.putBlock(b, blk); err != nil {
					return err
				}
				return f.touchDir(dir)
			}
			prev, off = off, off+rec
		}
	}
	return fs.ErrNotExist
}

// setDotDot points a directory's ".." (its first block's second entry) at parent.
func (f *FS) setDotDot(dir *inode, parent uint32) error {
	b, blk, err := f.dirBlock(dir, 0)
	if err != nil {
		return err
	}
	off := int(le.Uint16(b[deRecLen:]))
	if off+deName > len(b) || f.nameLenAt(b[off:]) != 2 || string(b[off+deName:off+deName+2]) != ".." {
		return fmt.Errorf("inode %d: no \"..\" where it belongs", dir.ino)
	}
	le.PutUint32(b[off+deInode:], parent)
	return f.putBlock(b, blk)
}

// touchDir marks dir changed (and no longer indexed) and writes it.
func (f *FS) touchDir(dir *inode) error {
	dir.flags &^= indexFlag
	dir.mtime = f.stamp()
	dir.ctime = dir.mtime
	return f.putInode(dir, false)
}

func (f *FS) stamp() uint32 { return uint32(f.now().Unix()) }
