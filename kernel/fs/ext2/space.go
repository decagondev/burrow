package ext2

// space.go - an ext2 volume's size and free space (W23-S03), from the
// superblock's block counts (kept current by every allocation). Docs:
// docs/04-plan/waves/WAVE-23-proc-virtio.md.

import "burrow/kernel/fsattr"

// Space is the volume's blocks, free and used.
func (f *FS) Space() fsattr.Space {
	f.mu.RLock()
	defer f.mu.RUnlock()
	total := uint64(f.sb.blocks) * uint64(f.sb.blockSize)
	free := uint64(le.Uint32(f.super[sFreeBlocks:])) * uint64(f.sb.blockSize)
	return fsattr.Space{Total: total, Free: free, Used: total - free}
}

// ReadOnly says whether writes are refused.
func (f *FS) ReadOnly() bool { return !f.Writable() }
