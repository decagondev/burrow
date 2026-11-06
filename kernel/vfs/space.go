package vfs

// space.go - what statfs and /proc/mounts ask of a mount (W23-S03): its
// size, from the file system's fsattr.Sizer, and whether it is read-only.
// Docs: docs/04-plan/waves/WAVE-23-proc-virtio.md.

import "burrow/kernel/fsattr"

// Space is the size of the file system holding name; ok is false when it
// does not know (devfs, procfs).
func (v *VFS) Space(name string) (fsattr.Space, bool) {
	m, _, found := v.resolve(name)
	if s, isSizer := m.fsys.(fsattr.Sizer); found && isSizer {
		return s.Space(), true
	}
	return fsattr.Space{}, false
}

// MountOf is the mount point holding name ("." for the root, "" for none):
// stat's st_dev tells mounts apart by it (W23-S03).
func (v *VFS) MountOf(name string) string {
	m, _, ok := v.resolve(name)
	if !ok {
		return ""
	}
	return m.point
}

// readOnly says whether fsys takes no writes: it says so, or it is not Writable.
func readOnly(fsys any) bool {
	if s, ok := fsys.(fsattr.Sizer); ok {
		return s.ReadOnly()
	}
	_, w := fsys.(Writable)
	return !w
}
