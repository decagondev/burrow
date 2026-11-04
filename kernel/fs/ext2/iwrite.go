package ext2

// iwrite.go - writing inodes and their data (W19-S02): an inode is
// rewritten in place (the fields this package does not know, such as the
// extended timestamps of a 256-byte inode, are kept); a file's data is
// replaced whole, as the VFS writes files, into blocks allocated in a row
// with each indirect block placed before the data it points to, as Linux
// does. A failed allocation gives back everything it took. Never frees a
// fast link's "blocks", which are its text. Docs: WAVE-19-unix.md.

import (
	"fmt"
	"syscall"
)

// putInode writes in; fresh starts from zeros (a new inode).
func (f *FS) putInode(in *inode, fresh bool) error {
	off := f.inodeOffset(in.ino)
	b := make([]byte, f.sb.inodeSize)
	if !fresh {
		if _, err := f.r.ReadAt(b, off); err != nil {
			return err
		}
	} else if f.sb.inodeSize > oldInodeSize {
		le.PutUint16(b[iExtraIsize:], min(f.sb.wantExtra, uint16(f.sb.inodeSize-oldInodeSize)))
	}
	le.PutUint16(b[iMode:], in.mode)
	le.PutUint16(b[iUID:], uint16(in.uid))
	le.PutUint16(b[iUIDHigh:], uint16(in.uid>>16))
	le.PutUint16(b[iGID:], uint16(in.gid))
	le.PutUint16(b[iGIDHigh:], uint16(in.gid>>16))
	le.PutUint32(b[iSize:], uint32(in.size))
	if in.isReg() {
		le.PutUint32(b[iSizeHigh:], uint32(in.size>>32))
	}
	le.PutUint16(b[iLinks:], in.links)
	le.PutUint32(b[iBlocks:], in.blocks512)
	le.PutUint32(b[iFlags:], in.flags)
	for o, v := range map[int]uint32{iAtime: in.atime, iCtime: in.ctime, iMtime: in.mtime, iDtime: in.dtime} {
		le.PutUint32(b[o:], v)
	}
	if !f.fastLink(in) {
		for i, p := range in.ptr {
			le.PutUint32(in.raw[4*i:], p)
		}
	}
	copy(b[iBlock:], in.raw[:])
	_, err := f.disk.WriteAt(b, off)
	return err
}

// path is where a file's block n hangs: the pointer in the inode, then the
// index at each level of indirect blocks below it.
func (f *FS) path(n uint64) (int, []uint64, error) {
	per := uint64(f.sb.blockSize / 4)
	switch {
	case n < nDirect:
		return int(n), nil, nil
	case n-nDirect < per:
		return nDirect, []uint64{n - nDirect}, nil
	case n-nDirect-per < per*per:
		n -= nDirect + per
		return nDirect + 1, []uint64{n / per, n % per}, nil
	case n-nDirect-per-per*per < per*per*per:
		n -= nDirect + per + per*per
		return nDirect + 2, []uint64{n / (per * per), n / per % per, n % per}, nil
	}
	return 0, nil, syscall.EFBIG
}

// tree places blocks into a file: the indirect blocks it reads or makes are
// kept until flush writes them; taken lists what it allocated, for undo.
type tree struct {
	f      *FS
	in     *inode
	goal   uint32
	tables map[uint32][]uint32
	taken  []uint32
	nTable int // indirect blocks allocated
}

func newTree(f *FS, in *inode, goal uint32) *tree {
	return &tree{f: f, in: in, goal: goal, tables: map[uint32][]uint32{}}
}

func (t *tree) alloc() (uint32, error) {
	blk, err := t.f.allocBlock(t.goal)
	if err != nil {
		return 0, err
	}
	t.goal = blk + 1
	t.taken = append(t.taken, blk)
	return blk, nil
}

func (t *tree) table(blk uint32) ([]uint32, error) {
	if p, ok := t.tables[blk]; ok {
		return p, nil
	}
	p, err := newBlockMap(t.f, t.in).pointers(blk)
	if err != nil {
		return nil, err
	}
	p = append([]uint32(nil), p...)
	t.tables[blk] = p
	return p, nil
}

// place allocates the file's block n (and the indirect blocks above it).
func (t *tree) place(n uint64) (uint32, error) {
	root, idx, err := t.f.path(n)
	if err != nil {
		return 0, err
	}
	p := &t.in.ptr[root]
	for _, i := range idx {
		if *p == 0 {
			blk, err := t.alloc()
			if err != nil {
				return 0, err
			}
			*p, t.tables[blk] = blk, make([]uint32, t.f.sb.blockSize/4)
			t.nTable++
		}
		tab, err := t.table(*p)
		if err != nil {
			return 0, err
		}
		p = &tab[i]
	}
	blk, err := t.alloc()
	*p = blk
	return blk, err
}

func (t *tree) flush() error {
	b := make([]byte, t.f.sb.blockSize)
	for blk, p := range t.tables {
		for i, v := range p {
			le.PutUint32(b[4*i:], v)
		}
		if _, err := t.f.disk.WriteAt(b, int64(blk)*t.f.sb.blockSize); err != nil {
			return err
		}
	}
	return nil
}

// undo gives back what place took and clears the inode's pointers.
func (t *tree) undo() {
	for _, blk := range t.taken {
		t.f.freeBlock(blk)
	}
	t.in.ptr = [nPointers]uint32{}
	t.in.blocks512 = t.f.aclSectors(t.in)
	t.in.size = 0
}

func (f *FS) aclSectors(in *inode) uint32 {
	if in.fileACL != 0 {
		return uint32(f.sb.blockSize / 512)
	}
	return 0
}

// setData replaces in's data with data (in memory; the caller writes in).
func (f *FS) setData(in *inode, data []byte) error {
	if err := f.freeData(in); err != nil {
		return err
	}
	bs := f.sb.blockSize
	n := (int64(len(data)) + bs - 1) / bs
	t := newTree(f, in, f.goal(in))
	start, prev := int64(0), uint32(0) // the run of blocks in a row being collected
	for i := int64(0); i <= n; i++ {
		var blk uint32
		if i < n {
			var err error
			if blk, err = t.place(uint64(i)); err != nil {
				t.undo()
				return err
			}
		}
		if i > start && (i == n || blk != prev+1) { // write the run [start, i)
			first := prev - uint32(i-1-start)
			if _, err := f.disk.WriteAt(data[start*bs:min(i*bs, int64(len(data)))], int64(first)*bs); err != nil {
				t.undo()
				return err
			}
			start = i
		}
		prev = blk
	}
	if err := t.flush(); err != nil {
		t.undo()
		return err
	}
	in.size = int64(len(data))
	in.blocks512 = uint32(n+int64(t.nTable))*uint32(bs/512) + f.aclSectors(in)
	return nil
}

// goal is where a file's blocks should start: its inode's group.
func (f *FS) goal(in *inode) uint32 {
	return f.sb.firstDataBlock + (in.ino-1)/f.sb.inodesPerGroup*f.sb.blocksPerGroup
}

// freeData gives back every block of in (data and indirect).
func (f *FS) freeData(in *inode) error {
	if !f.fastLink(in) {
		for i, p := range in.ptr {
			if p == 0 {
				continue
			}
			var err error
			if i < nDirect {
				err = f.freeBlock(p)
			} else {
				err = f.freeTable(in, p, i-nDirect+1)
			}
			if err != nil {
				return fmt.Errorf("inode %d: %w", in.ino, err)
			}
		}
		in.ptr = [nPointers]uint32{}
	}
	in.blocks512 = f.aclSectors(in)
	return nil
}

func (f *FS) freeTable(in *inode, blk uint32, levels int) error {
	p, err := newBlockMap(f, in).pointers(blk)
	if err != nil {
		return err
	}
	for _, c := range p {
		if c == 0 {
			continue
		}
		if levels > 1 {
			err = f.freeTable(in, c, levels-1)
		} else {
			err = f.freeBlock(c)
		}
		if err != nil {
			return err
		}
	}
	return f.freeBlock(blk)
}
