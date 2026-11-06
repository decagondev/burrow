package fat

// space.go - a FAT32 volume's size and free space (W23-S03): its clusters
// and the free ones in the FAT. Docs: docs/04-plan/waves/WAVE-23-proc-virtio.md.

import "burrow/kernel/fsattr"

// Space is the data area's clusters, free and used.
func (f *FS) Space() fsattr.Space {
	f.mu.RLock()
	defer f.mu.RUnlock()
	total := uint64(len(f.fat)-2) * uint64(f.clusterBytes)
	free := uint64(f.freeClusters()) * uint64(f.clusterBytes)
	return fsattr.Space{Total: total, Free: free, Used: total - free}
}

// ReadOnly says whether the volume was mounted without a disk to write.
func (f *FS) ReadOnly() bool { return f.disk == nil }
