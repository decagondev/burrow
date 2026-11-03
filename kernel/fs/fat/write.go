package fat

// write.go - the write side of a mounted FAT32 volume (W13-S04): whole-file
// WriteFile, Mkdir and Remove (vfs.Writable), done in an order that keeps
// the volume consistent if the machine stops halfway: data first, then the
// directory entry that points at it, then the FAT sectors the operation
// changed (every copy) and the FSInfo hints, then a flush. At worst a crash
// loses clusters (fsck.fat reclaims them); an entry never points at data
// that was not written. Never writes on a mount made with Mount.
// Clusters are here; directory entries are in dirwrite.go.

import (
	"errors"
	"io"
	"io/fs"
	"syscall"
)

// Disk is what a writable mount needs: byte access both ways and a flush.
type Disk interface {
	io.ReaderAt
	io.WriterAt
	Flush() error
}

// MountRW mounts the volume on d for reading and writing.
func MountRW(d Disk) (*FS, error) {
	f, err := Mount(d)
	if err != nil {
		return nil, err
	}
	f.disk = d
	return f, nil
}

const (
	fsiLeadSig   = 0x41615252
	fsiStrucSig  = 0x61417272
	fsiFreeCount = 488
	fsiNextFree  = 492
)

func (f *FS) setFAT(c, v uint32) {
	f.fat[c] = v
	f.dirty[int64(c)*4/sectorSize] = true
}

// alloc takes one free cluster and marks it the end of a chain.
func (f *FS) alloc() (uint32, error) {
	n := uint32(len(f.fat))
	for i := uint32(0); i < n-2; i++ {
		c := 2 + (f.nextFree-2+i)%(n-2)
		if f.fat[c] == 0 {
			f.setFAT(c, fatMask)
			f.nextFree = c + 1
			return c, nil
		}
	}
	return 0, syscall.ENOSPC
}

// resize makes the chain starting at first exactly want clusters long and
// returns its (possibly new) first cluster, 0 for an empty one. Freed
// clusters go back to the FAT; on ENOSPC nothing has changed.
func (f *FS) resize(first uint32, want int) (uint32, []uint32, error) {
	chain, err := f.chain(first)
	if err != nil {
		return 0, nil, err
	}
	switch {
	case want < len(chain):
		for _, c := range chain[want:] {
			f.setFAT(c, 0)
		}
		if want == 0 {
			return 0, nil, nil
		}
		f.setFAT(chain[want-1], fatMask)
		return first, chain[:want], nil
	case want > len(chain):
		had := len(chain)
		for len(chain) < want {
			c, err := f.alloc()
			if err != nil { // undo this call's allocations: the chain is as it was
				for _, back := range chain[had:] {
					f.setFAT(back, 0)
				}
				if had > 0 {
					f.setFAT(chain[had-1], fatMask)
				}
				return 0, nil, err
			}
			if len(chain) > 0 {
				f.setFAT(chain[len(chain)-1], c)
			}
			chain = append(chain, c)
		}
	}
	if len(chain) == 0 {
		return 0, nil, nil
	}
	return chain[0], chain, nil
}

func (f *FS) clustersFor(n int) int { return int((int64(n) + f.clusterBytes - 1) / f.clusterBytes) }

func (f *FS) clusterOffset(c uint32) int64 { return f.dataStart + int64(c-2)*f.clusterBytes }

// writeData writes data over the clusters of chain, in order.
func (f *FS) writeData(chain []uint32, data []byte) error {
	for i, c := range chain {
		part := data[min(int64(i)*f.clusterBytes, int64(len(data))):min(int64(i+1)*f.clusterBytes, int64(len(data)))]
		if _, err := f.disk.WriteAt(part, f.clusterOffset(c)); err != nil {
			return err
		}
	}
	return nil
}

// zeroCluster clears a cluster (a new directory's end-of-entries marks).
func (f *FS) zeroCluster(c uint32) error {
	_, err := f.disk.WriteAt(make([]byte, f.clusterBytes), f.clusterOffset(c))
	return err
}

// freeClusters counts the free clusters.
func (f *FS) freeClusters() int {
	n := 0
	for _, v := range f.fat[2:] {
		if v == 0 {
			n++
		}
	}
	return n
}

// commit writes the changed FAT sectors to every FAT copy, then FSInfo's
// free count and next-free hint, then flushes the disk.
func (f *FS) commit() error {
	sector := make([]byte, sectorSize)
	for s := range f.dirty {
		for i := range sector {
			sector[i] = 0
		}
		for k := int64(0); k < sectorSize/4; k++ {
			if c := s*sectorSize/4 + k; c < int64(len(f.fat)) {
				le.PutUint32(sector[4*k:], f.fat[c])
			}
		}
		if s == 0 { // entries 0 and 1 hold the media byte and flags, not clusters
			if _, err := f.r.ReadAt(sector[:8], f.fatStart); err != nil {
				return err
			}
		}
		for copyN := 0; copyN < f.numFATs; copyN++ {
			if _, err := f.disk.WriteAt(sector, f.fatStart+int64(copyN)*f.fatBytes+s*sectorSize); err != nil {
				return err
			}
		}
		delete(f.dirty, s)
	}
	if err := f.writeFSInfo(); err != nil {
		return err
	}
	return f.disk.Flush()
}

func (f *FS) writeFSInfo() error {
	if f.fsInfo == 0 {
		return nil
	}
	b := make([]byte, sectorSize)
	if _, err := f.r.ReadAt(b, f.fsInfo); err != nil {
		return err
	}
	if le.Uint32(b[0:]) != fsiLeadSig || le.Uint32(b[484:]) != fsiStrucSig {
		return nil // not an FSInfo sector we understand: leave it alone
	}
	le.PutUint32(b[fsiFreeCount:], uint32(f.freeClusters()))
	le.PutUint32(b[fsiNextFree:], f.nextFree)
	_, err := f.disk.WriteAt(b[fsiFreeCount:fsiNextFree+4], f.fsInfo+fsiFreeCount)
	return err
}

// writable checks the mount can write (f.mu held).
func (f *FS) writable(op, name string) error {
	if f.disk == nil {
		return &fs.PathError{Op: op, Path: name, Err: fs.ErrPermission}
	}
	if !fs.ValidPath(name) || name == "." {
		return &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	return nil
}

var errNotDir = errors.New("fat: not a directory")
