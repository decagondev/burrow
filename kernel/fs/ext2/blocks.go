package ext2

// blocks.go - a file's blocks (W19-S01): the twelve direct pointers, then
// the single, double and triple indirect blocks, with indirect blocks
// cached per open file; and reading byte ranges as runs of blocks that lie
// in a row on the disk, so a file written in one go costs few disk reads
// (the lesson of W15-S05). A zero pointer is a hole and reads as zeros.
// Never follows a pointer past the end of the volume. Docs: WAVE-19-unix.md.

import (
	"fmt"
	"io"
	"sync"
)

type blockMap struct {
	f     *FS
	in    *inode
	mu    sync.Mutex
	cache map[uint32][]uint32 // indirect block -> its pointers
}

func newBlockMap(f *FS, in *inode) *blockMap {
	return &blockMap{f: f, in: in, cache: map[uint32][]uint32{}}
}

// pointers is the content of indirect block blk.
func (m *blockMap) pointers(blk uint32) ([]uint32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.cache[blk]; ok {
		return p, nil
	}
	if blk >= m.f.sb.blocks {
		return nil, fmt.Errorf("inode %d: indirect block %d past the volume", m.in.ino, blk)
	}
	b := make([]byte, m.f.sb.blockSize)
	if _, err := m.f.r.ReadAt(b, int64(blk)*m.f.sb.blockSize); err != nil {
		return nil, err
	}
	p := make([]uint32, len(b)/4)
	for i := range p {
		p[i] = le.Uint32(b[4*i:])
	}
	m.cache[blk] = p
	return p, nil
}

// lookup is the disk block holding the file's block n (0: a hole).
func (m *blockMap) lookup(n uint64) (uint32, error) {
	per := uint64(m.f.sb.blockSize / 4)
	if n < nDirect {
		return m.check(m.in.ptr[n])
	}
	n -= nDirect
	blk, levels := m.in.ptr[nDirect], 1
	switch {
	case n < per:
	case n-per < per*per:
		n, blk, levels = n-per, m.in.ptr[nDirect+1], 2
	case n-per-per*per < per*per*per:
		n, blk, levels = n-per-per*per, m.in.ptr[nDirect+2], 3
	default:
		return 0, fmt.Errorf("inode %d: block %d beyond the triple indirect block", m.in.ino, n)
	}
	for l := levels - 1; l >= 0; l-- {
		if blk == 0 {
			return 0, nil
		}
		p, err := m.pointers(blk)
		if err != nil {
			return 0, err
		}
		span := uint64(1)
		for range l {
			span *= per
		}
		blk = p[n/span]
		n %= span
	}
	return m.check(blk)
}

func (m *blockMap) check(blk uint32) (uint32, error) {
	if blk >= m.f.sb.blocks {
		return 0, fmt.Errorf("inode %d: block %d past the volume", m.in.ino, blk)
	}
	return blk, nil
}

// readAt fills p from the file's byte off, up to its size: holes as zeros,
// blocks that follow each other on the disk in one read.
func (m *blockMap) readAt(p []byte, off int64) (int, error) {
	bs, size := m.f.sb.blockSize, m.in.size
	done := 0
	for done < len(p) && off < size {
		first, in := uint64(off/bs), off%bs
		want := min(int64(len(p)-done), size-off)
		blk, err := m.lookup(first)
		if err != nil {
			return done, err
		}
		run := int64(1) // blocks from blk on that follow it on the disk (or holes that follow a hole)
		for run*bs-in < want {
			next, err := m.lookup(first + uint64(run))
			if err != nil || (blk == 0) != (next == 0) || blk != 0 && next != blk+uint32(run) {
				break
			}
			run++
		}
		n := min(want, run*bs-in)
		if blk == 0 {
			clear(p[done : done+int(n)])
		} else if _, err := m.f.r.ReadAt(p[done:done+int(n)], int64(blk)*bs+in); err != nil {
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
