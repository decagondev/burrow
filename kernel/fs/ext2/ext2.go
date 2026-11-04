// Package ext2 reads the second extended file system (W19-S01): a Unix
// file system with owners, modes, symbolic and hard links and sparse
// files, which e2fsprogs creates, checks and repairs on the host. It is an
// io/fs.FS and an fs.ReadLinkFS; fs.FileInfo.Sys() is a vfs.Attr. Revision
// 0 and 1 volumes without a journal or extents; an unknown incompatible
// feature is refused, an unknown read-only-compatible one is reported in
// ReadOnlyFeatures and keeps it read-only. Mounted with MountRW it writes
// (W19-S02, write.go); Mount never writes. Never decides permissions: it
// reports owners, kernel/proc checks them.
// Docs: docs/03-decisions/ADR-0010-ext2.md, WAVE-19-unix.md.
package ext2

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"
	"syscall"
	"time"
)

// FS is a mounted ext2 volume. It keeps the geometry, and for writing the
// superblock, the group descriptors and the bitmaps it has read; inodes,
// directories and data are read from the disk when asked for. Safe for
// concurrent use: writes are serialized, lookups share a lock.
type FS struct {
	r       io.ReaderAt
	sb      super
	tables  []int64 // byte offset of each group's inode table
	hasType bool    // directory entries carry the file's type
	Label   string

	mu      sync.RWMutex
	disk    Disk              // nil: read-only
	super   []byte            // the superblock as read; its free counts kept current
	gdt     []byte            // the group descriptors as read, kept current
	bitmaps map[uint32][]byte // bitmap blocks read so far, by block number
	dirty   map[uint32]bool   // bitmap blocks changed since the last commit
	now     func() time.Time
	// ReadOnlyFeatures: the volume uses a feature this package could not
	// keep consistent when writing; it must stay read-only.
	ReadOnlyFeatures bool
}

// Probe says whether r starts like an ext2 (or ext3/4) volume.
func Probe(r io.ReaderAt) bool {
	var b [2]byte
	_, err := r.ReadAt(b[:], superOffset+sMagic)
	return err == nil && le.Uint16(b[:]) == magic
}

// Mount reads the superblock and the group descriptors of the volume behind r.
func Mount(r io.ReaderAt) (*FS, error) {
	b := make([]byte, superSize)
	if _, err := r.ReadAt(b, superOffset); err != nil {
		return nil, fmt.Errorf("ext2: superblock: %w", err)
	}
	sb, err := parseSuper(b)
	if err != nil {
		return nil, err
	}
	gdt := make([]byte, int64(sb.groups)*groupDescSize)
	if _, err := r.ReadAt(gdt, int64(sb.firstDataBlock+1)*sb.blockSize); err != nil {
		return nil, fmt.Errorf("ext2: group descriptors: %w", err)
	}
	f := &FS{r: r, sb: sb, hasType: sb.incompat&incompatFiletype != 0, Label: sb.label,
		ReadOnlyFeatures: sb.roCompat&^knownROCompat != 0, super: b, gdt: gdt,
		bitmaps: map[uint32][]byte{}, dirty: map[uint32]bool{}, now: time.Now}
	for g := range sb.groups {
		blk := le.Uint32(gdt[g*groupDescSize+bgInodeTable:])
		if blk == 0 || blk >= sb.blocks {
			return nil, fmt.Errorf("ext2: group %d: inode table at block %d", g, blk)
		}
		f.tables = append(f.tables, int64(blk)*sb.blockSize)
	}
	root, err := f.inode(rootIno)
	if err != nil || !root.isDir() {
		return nil, fmt.Errorf("ext2: the root is not a directory (%v)", err)
	}
	return f, nil
}

// errNotLink is what ReadLink says about a file that is not a link.
var errNotLink = fs.ErrInvalid

// walk resolves name (an io/fs path) from the root, following symbolic
// links on the way, and at the end too when followLast is set. A link's
// target is read relative to the link's directory, or from the volume's
// root when absolute; ".." in a target goes back along the path walked, and
// stops at the root. kernel/vfs resolves links across mounts (W19-S05);
// this is for the volume on its own.
func (f *FS) walk(name string, followLast bool) (*inode, error) {
	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}
	root, err := f.inode(rootIno)
	if err != nil {
		return nil, err
	}
	dirs := []*inode{root} // the directories from the root to the current one
	cur, parts, links := root, split(name), 0
	for len(parts) > 0 {
		p := parts[0]
		parts = parts[1:]
		if p == "" || p == "." {
			continue
		}
		if !cur.isDir() {
			return nil, fs.ErrNotExist
		}
		if p == ".." {
			if len(dirs) > 1 {
				dirs = dirs[:len(dirs)-1]
			}
			cur = dirs[len(dirs)-1]
			continue
		}
		next, err := f.child(cur, p)
		if err != nil {
			return nil, err
		}
		if next.isLink() && (len(parts) > 0 || followLast) {
			if links++; links > maxLinks {
				return nil, syscall.ELOOP
			}
			target, err := f.linkTarget(next)
			if err != nil {
				return nil, err
			}
			if len(target) > 0 && target[0] == '/' {
				dirs = dirs[:1]
			}
			parts = append(split(target), parts...)
			cur = dirs[len(dirs)-1]
			continue
		}
		if next.isDir() {
			dirs = append(dirs, next)
		}
		cur = next
	}
	return cur, nil
}

func split(p string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(p); i++ {
		if i == len(p) || p[i] == '/' {
			out = append(out, p[start:i])
			start = i + 1
		}
	}
	return out
}

func base(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '/' {
			return name[i+1:]
		}
	}
	return name
}

func pathErr(op, name string, err error) error {
	if errors.Is(err, syscall.ELOOP) || errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid) {
		return &fs.PathError{Op: op, Path: name, Err: err}
	}
	return &fs.PathError{Op: op, Path: name, Err: fmt.Errorf("ext2: %w", err)}
}

// Open implements fs.FS; it follows symbolic links.
func (f *FS) Open(name string) (fs.File, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	in, err := f.walk(name, true)
	if err != nil {
		return nil, pathErr("open", name, err)
	}
	if in.isDir() {
		ents, err := f.readDir(in)
		if err != nil {
			return nil, pathErr("open", name, err)
		}
		return &dir{fsys: f, info: f.info(base(name), in), ents: ents}, nil
	}
	return &file{fsys: f, in: in, name: base(name), m: newBlockMap(f, in)}, nil
}

// Stat implements fs.StatFS; it follows symbolic links.
func (f *FS) Stat(name string) (fs.FileInfo, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	in, err := f.walk(name, true)
	if err != nil {
		return nil, pathErr("stat", name, err)
	}
	return f.info(base(name), in), nil
}

// Lstat implements fs.ReadLinkFS: a final symbolic link is described itself.
func (f *FS) Lstat(name string) (fs.FileInfo, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	in, err := f.walk(name, false)
	if err != nil {
		return nil, pathErr("lstat", name, err)
	}
	return f.info(base(name), in), nil
}

// ReadLink implements fs.ReadLinkFS.
func (f *FS) ReadLink(name string) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	in, err := f.walk(name, false)
	if err != nil {
		return "", pathErr("readlink", name, err)
	}
	if !in.isLink() {
		return "", pathErr("readlink", name, errNotLink)
	}
	target, err := f.linkTarget(in)
	if err != nil {
		return "", pathErr("readlink", name, err)
	}
	return target, nil
}
