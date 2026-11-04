package ext2

// alloc.go - allocation (W19-S02): the block and inode bitmaps of each
// group, read once and written back at commit, and the free counts in the
// group descriptors and the superblock kept in step with them. Blocks are
// taken from a goal onwards (the block after the last one a file got, so a
// file written in one go lies in a row); a directory's inode goes to the
// group with the most free inodes, a file's to its directory's group.
// Never hands out a block past the volume or a reserved inode, and never
// frees one twice: either is a corrupt volume, reported as an error.
// Docs: WAVE-19-unix.md.

import (
	"fmt"
	"syscall"
)

// gd is group g's descriptor, as stored.
func (f *FS) gd(g uint32) []byte { return f.gdt[g*groupDescSize : (g+1)*groupDescSize] }

// add changes a 16-bit count in a group descriptor and the superblock's
// 32-bit count beside it.
func (f *FS) add(g uint32, gdOff, sbOff int, delta int) {
	d := f.gd(g)
	le.PutUint16(d[gdOff:], uint16(int(le.Uint16(d[gdOff:]))+delta))
	if sbOff >= 0 {
		le.PutUint32(f.super[sbOff:], uint32(int(le.Uint32(f.super[sbOff:]))+delta))
	}
}

// bitmap is the content of bitmap block blk, read once.
func (f *FS) bitmap(blk uint32) ([]byte, error) {
	if b, ok := f.bitmaps[blk]; ok {
		return b, nil
	}
	if blk == 0 || blk >= f.sb.blocks {
		return nil, fmt.Errorf("a bitmap at block %d", blk)
	}
	b := make([]byte, f.sb.blockSize)
	if _, err := f.r.ReadAt(b, int64(blk)*f.sb.blockSize); err != nil {
		return nil, err
	}
	f.bitmaps[blk] = b
	return b, nil
}

func bit(b []byte, i uint32) bool { return b[i/8]&(1<<(i%8)) != 0 }

// groupBlocks is how many blocks group g has (the last may be short).
func (f *FS) groupBlocks(g uint32) uint32 {
	return min(f.sb.blocksPerGroup, f.sb.blocks-f.sb.firstDataBlock-g*f.sb.blocksPerGroup)
}

// allocBlock takes a free block, at goal or after it if it can.
func (f *FS) allocBlock(goal uint32) (uint32, error) {
	first, per := f.sb.firstDataBlock, f.sb.blocksPerGroup
	if goal < first || goal >= f.sb.blocks {
		goal = first
	}
	g0 := (goal - first) / per
	for k := uint32(0); k <= f.sb.groups; k++ { // the goal's group from the goal, the others, then the goal's group again
		g := (g0 + k) % f.sb.groups
		start := uint32(0)
		if k == 0 {
			start = (goal - first) % per
		}
		if le.Uint16(f.gd(g)[bgFreeBlocks:]) == 0 {
			continue
		}
		bmBlk := le.Uint32(f.gd(g)[bgBlockBitmap:])
		bm, err := f.bitmap(bmBlk)
		if err != nil {
			return 0, err
		}
		for i := start; i < f.groupBlocks(g); i++ {
			if !bit(bm, i) {
				bm[i/8] |= 1 << (i % 8)
				f.dirty[bmBlk] = true
				f.add(g, bgFreeBlocks, sFreeBlocks, -1)
				return first + g*per + i, nil
			}
		}
	}
	return 0, syscall.ENOSPC
}

func (f *FS) freeBlock(blk uint32) error {
	if blk < f.sb.firstDataBlock || blk >= f.sb.blocks {
		return fmt.Errorf("freeing block %d, past the volume", blk)
	}
	g, i := (blk-f.sb.firstDataBlock)/f.sb.blocksPerGroup, (blk-f.sb.firstDataBlock)%f.sb.blocksPerGroup
	bmBlk := le.Uint32(f.gd(g)[bgBlockBitmap:])
	bm, err := f.bitmap(bmBlk)
	if err != nil {
		return err
	}
	if !bit(bm, i) {
		return fmt.Errorf("block %d freed twice", blk)
	}
	bm[i/8] &^= 1 << (i % 8)
	f.dirty[bmBlk] = true
	f.add(g, bgFreeBlocks, sFreeBlocks, 1)
	return nil
}

// allocInode takes a free inode for a directory (in the group with the
// most free inodes, which spreads directories out) or a file (near its
// directory, parent).
func (f *FS) allocInode(dir bool, parent uint32) (uint32, error) {
	g0 := (parent - 1) / f.sb.inodesPerGroup
	if dir {
		for g := range f.sb.groups {
			if le.Uint16(f.gd(g)[bgFreeInodes:]) > le.Uint16(f.gd(g0)[bgFreeInodes:]) {
				g0 = g
			}
		}
	}
	for k := range f.sb.groups {
		g := (g0 + k) % f.sb.groups
		if le.Uint16(f.gd(g)[bgFreeInodes:]) == 0 {
			continue
		}
		bmBlk := le.Uint32(f.gd(g)[bgInodeBitmap:])
		bm, err := f.bitmap(bmBlk)
		if err != nil {
			return 0, err
		}
		for i := range f.sb.inodesPerGroup {
			ino := g*f.sb.inodesPerGroup + i + 1
			if bit(bm, i) || ino < f.sb.firstIno || ino > f.sb.inodes {
				continue
			}
			bm[i/8] |= 1 << (i % 8)
			f.dirty[bmBlk] = true
			f.add(g, bgFreeInodes, sFreeInodes, -1)
			if dir {
				f.add(g, bgUsedDirs, -1, 1)
			}
			return ino, nil
		}
	}
	return 0, syscall.ENOSPC
}

func (f *FS) freeInode(ino uint32, dir bool) error {
	g, i := (ino-1)/f.sb.inodesPerGroup, (ino-1)%f.sb.inodesPerGroup
	if ino < f.sb.firstIno || ino > f.sb.inodes {
		return fmt.Errorf("freeing inode %d, reserved or past the volume", ino)
	}
	bmBlk := le.Uint32(f.gd(g)[bgInodeBitmap:])
	bm, err := f.bitmap(bmBlk)
	if err != nil {
		return err
	}
	if !bit(bm, i) {
		return fmt.Errorf("inode %d freed twice", ino)
	}
	bm[i/8] &^= 1 << (i % 8)
	f.dirty[bmBlk] = true
	f.add(g, bgFreeInodes, sFreeInodes, 1)
	if dir {
		f.add(g, bgUsedDirs, -1, -1)
	}
	return nil
}
