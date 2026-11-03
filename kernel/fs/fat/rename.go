package fat

// rename.go - Rename on a writable FAT32 volume (W13-S05), with rename(2)'s
// rules: a file replaces a file, a directory replaces an empty directory,
// a directory never moves into itself. The new entry is written before the
// old one is deleted, so a crash leaves the file reachable (under both
// names at worst, which fsck.fat reports and repairs); a replaced file's
// clusters are freed last. A moved directory's ".." follows it.

import (
	"io/fs"
	"strings"
	"syscall"
)

// Rename moves oldname to newname.
func (f *FS) Rename(oldname, newname string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range []string{oldname, newname} {
		if err := f.writable("rename", n); err != nil {
			return err
		}
	}
	odir, obase, err := f.parent("rename", oldname)
	if err != nil {
		return err
	}
	ndir, nbase, err := f.parent("rename", newname)
	if err != nil {
		return err
	}
	od, err := f.scan(odir.first)
	if err != nil {
		return err
	}
	src := find(od, obase)
	if src == nil {
		return &fs.PathError{Op: "rename", Path: oldname, Err: fs.ErrNotExist}
	}
	if oldname == newname {
		return nil
	}
	if src.e.dir && strings.HasPrefix(strings.ToLower(newname), strings.ToLower(oldname)+"/") {
		return &fs.PathError{Op: "rename", Path: newname, Err: syscall.EINVAL}
	}
	nd := od
	if ndir.first != odir.first {
		if nd, err = f.scan(ndir.first); err != nil {
			return err
		}
	}
	dst := find(nd, nbase)
	sameEntry := dst != nil && ndir.first == odir.first && dst.start == src.start
	if dst != nil && !sameEntry {
		if err := f.replaceable(src, dst); err != nil {
			return &fs.PathError{Op: "rename", Path: newname, Err: err}
		}
	}
	return f.move(odir, od, src, ndir, nd, nbase, dst, sameEntry)
}

// replaceable is rename(2)'s rule for src moving onto an existing dst.
func (f *FS) replaceable(src, dst *dirRec) error {
	switch {
	case src.e.dir && !dst.e.dir:
		return syscall.ENOTDIR
	case !src.e.dir && dst.e.dir:
		return syscall.EISDIR
	case dst.e.dir:
		inside, err := f.readDir(dst.e.first)
		if err != nil {
			return err
		}
		if len(inside) > 0 {
			return syscall.ENOTEMPTY
		}
	}
	return nil
}

// move writes the new entry, deletes the old one (and a replaced one),
// points a moved directory's ".." at its new parent, and commits.
func (f *FS) move(odir *entry, od *dirData, src *dirRec, ndir *entry, nd *dirData, nbase string, dst *dirRec, sameEntry bool) error {
	deleteSlots := func(d *dirData, r *dirRec) error {
		for i := r.start; i <= r.end; i++ {
			if _, err := f.disk.WriteAt([]byte{entryDeleted}, d.offs[i]); err != nil {
				return err
			}
			d.raw[i*dirEntry] = entryDeleted // insert below may reuse these slots
		}
		return nil
	}
	if sameEntry || dst != nil { // the old name and a replaced entry make room first
		if err := deleteSlots(nd, dst); err != nil {
			return err
		}
	}
	if err := f.insert(ndir, nd, nbase, src.short[dirAttr], src.e.first, uint32(src.e.size), src.e.modTime); err != nil {
		return err
	}
	if !sameEntry {
		if err := deleteSlots(od, src); err != nil {
			return err
		}
	}
	if src.e.dir && ndir.first != odir.first {
		parentRef := ndir.first
		if parentRef == f.rootCluster {
			parentRef = 0
		}
		e := make([]byte, 4)
		le.PutUint16(e[0:], uint16(parentRef>>16))
		le.PutUint16(e[2:], uint16(parentRef))
		dotdot := f.clusterOffset(src.e.first) + dirEntry
		if _, err := f.disk.WriteAt(e[0:2], dotdot+dirClusHi); err != nil {
			return err
		}
		if _, err := f.disk.WriteAt(e[2:4], dotdot+dirClusLo); err != nil {
			return err
		}
	}
	if dst != nil && !sameEntry {
		if _, _, err := f.resize(dst.e.first, 0); err != nil {
			return err
		}
	}
	return f.commit()
}
