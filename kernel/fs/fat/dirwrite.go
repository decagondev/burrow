package fat

// dirwrite.go - creating, changing and removing directory entries
// (W13-S04): WriteFile, Mkdir and Remove for vfs.Writable. New entries get
// a long name (case and length survive) and a unique NAME~N short alias, as
// Format makes them; they go into the first run of free slots, and a full
// directory grows by a zeroed cluster. Never leaves an entry pointing at
// clusters that were not written (write.go has the order).

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
	"syscall"
	"time"
)

// parent finds the directory that holds name and name's last element.
func (f *FS) parent(op, name string) (*entry, string, error) {
	dirName, base := path.Split(name)
	dirName = strings.TrimSuffix(dirName, "/")
	if dirName == "" {
		dirName = "."
	}
	d, err := f.lookup(dirName)
	if err != nil {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: err}
	}
	if !d.dir {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: errNotDir}
	}
	return d, base, nil
}

func find(d *dirData, name string) *dirRec {
	for i := range d.recs {
		if r := &d.recs[i]; strings.EqualFold(r.e.name, name) {
			return r
		}
	}
	return nil
}

// WriteFile makes name hold data, creating it or replacing its contents.
func (f *FS) WriteFile(name string, data []byte, _ fs.FileMode, mtime time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.writable("write", name); err != nil {
		return err
	}
	dir, base, err := f.parent("write", name)
	if err != nil {
		return err
	}
	d, err := f.scan(dir.first)
	if err != nil {
		return err
	}
	old := find(d, base)
	if old != nil && old.e.dir {
		return &fs.PathError{Op: "write", Path: name, Err: syscall.EISDIR}
	}
	var first uint32
	if old != nil {
		first = old.e.first
	}
	first, chain, err := f.resize(first, f.clustersFor(len(data)))
	if err != nil {
		f.dirty = map[int64]bool{} // nothing to commit: resize undid itself
		return &fs.PathError{Op: "write", Path: name, Err: err}
	}
	if err := f.writeData(chain, data); err != nil {
		return err
	}
	if old != nil {
		e := shortEntry(string(old.short[:11]), old.short[dirAttr], first, uint32(len(data)), mtime)
		e[dirNTRes] = old.short[dirNTRes]
		if _, err := f.disk.WriteAt(e, d.offs[old.end]); err != nil {
			return err
		}
	} else if err := f.insert(dir, d, base, attrArchive, first, uint32(len(data)), mtime); err != nil {
		return err
	}
	return f.commit()
}

// Mkdir makes an empty directory: one zeroed cluster holding "." and "..".
func (f *FS) Mkdir(name string, _ fs.FileMode, mtime time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.writable("mkdir", name); err != nil {
		return err
	}
	dir, base, err := f.parent("mkdir", name)
	if err != nil {
		return err
	}
	d, err := f.scan(dir.first)
	if err != nil {
		return err
	}
	if find(d, base) != nil {
		return &fs.PathError{Op: "mkdir", Path: name, Err: fs.ErrExist}
	}
	c, err := f.alloc()
	if err != nil {
		return &fs.PathError{Op: "mkdir", Path: name, Err: err}
	}
	parentRef := dir.first
	if parentRef == f.rootCluster {
		parentRef = 0 // ".." of a directory in the root points at cluster 0
	}
	body := make([]byte, f.clusterBytes)
	copy(body, shortEntry(".          ", attrDirectory, c, 0, mtime))
	copy(body[dirEntry:], shortEntry("..         ", attrDirectory, parentRef, 0, mtime))
	if _, err := f.disk.WriteAt(body, f.clusterOffset(c)); err != nil {
		return err
	}
	if err := f.insert(dir, d, base, attrDirectory, c, 0, mtime); err != nil {
		return err
	}
	return f.commit()
}

// Remove deletes a file, or a directory that holds nothing.
func (f *FS) Remove(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.writable("remove", name); err != nil {
		return err
	}
	dir, base, err := f.parent("remove", name)
	if err != nil {
		return err
	}
	d, err := f.scan(dir.first)
	if err != nil {
		return err
	}
	r := find(d, base)
	if r == nil {
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrNotExist}
	}
	if r.e.dir {
		inside, err := f.readDir(r.e.first)
		if err != nil {
			return err
		}
		if len(inside) > 0 {
			return &fs.PathError{Op: "remove", Path: name, Err: syscall.ENOTEMPTY}
		}
	}
	for i := r.start; i <= r.end; i++ { // the entry goes first: then its clusters are free
		if _, err := f.disk.WriteAt([]byte{entryDeleted}, d.offs[i]); err != nil {
			return err
		}
	}
	if _, _, err := f.resize(r.e.first, 0); err != nil {
		return err
	}
	return f.commit()
}

// insert writes name's long and short entries into the directory dir
// (decoded as d), in the first run of free slots, growing it if needed.
func (f *FS) insert(dir *entry, d *dirData, name string, attr byte, first, size uint32, mtime time.Time) error {
	short := f.alias(d, name)
	ents := append(longEntries(name, shortChecksum([]byte(short))), shortEntry(short, attr, first, size, mtime)...)
	need := len(ents) / dirEntry
	run := 0
	for i := range d.offs {
		if b := d.raw[i*dirEntry]; b == entryEnd || b == entryDeleted {
			run++
		} else {
			run = 0
		}
		if run == need {
			return f.writeSlots(d.offs[i+1-need:i+1], ents)
		}
	}
	grow := f.clustersFor((need - run) * dirEntry)
	chain, err := f.chain(dir.first)
	if err != nil {
		return err
	}
	_, longer, err := f.resize(dir.first, len(chain)+grow)
	if err != nil {
		return err
	}
	offs := d.offs[len(d.offs)-run:]
	for _, c := range longer[len(chain):] {
		if err := f.zeroCluster(c); err != nil {
			return err
		}
		for off := int64(0); off < f.clusterBytes; off += dirEntry {
			offs = append(offs, f.clusterOffset(c)+off)
		}
	}
	return f.writeSlots(offs[:need], ents)
}

func (f *FS) writeSlots(offs []int64, ents []byte) error {
	for i, off := range offs {
		if _, err := f.disk.WriteAt(ents[i*dirEntry:(i+1)*dirEntry], off); err != nil {
			return err
		}
	}
	return nil
}

// alias is a short 8.3 name for name that no entry of d uses: BASE~N.
func (f *FS) alias(d *dirData, name string) string {
	taken := map[string]bool{}
	for _, r := range d.recs {
		taken[string(r.short[:11])] = true
	}
	base := shortBase(name)
	for i := 1; ; i++ {
		tail := fmt.Sprintf("~%d", i)
		s := fmt.Sprintf("%-11s", base[:min(len(base), 8-len(tail))]+tail)
		if !taken[s] {
			return s
		}
	}
}
