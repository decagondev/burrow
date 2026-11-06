package memfs

// space.go - the size of a tree in memory (W23-S03): what its files use;
// read-only (an initrd) it is exactly that full, writable only RAM limits
// it (Total 0). Docs: docs/04-plan/waves/WAVE-23-proc-virtio.md.

import "burrow/kernel/fsattr"

// Space is the bytes its files hold.
func (f *FS) Space() fsattr.Space {
	_, _, used := f.Counts()
	if f.ReadOnly() {
		return fsattr.Space{Total: uint64(used), Used: uint64(used)}
	}
	return fsattr.Space{Used: uint64(used)}
}

// ReadOnly says whether writes are refused (an initrd).
func (f *FS) ReadOnly() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.readOnly
}
