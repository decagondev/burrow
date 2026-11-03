package fat

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// FS is a mounted FAT32 volume. The FAT is read once at Mount and kept in
// memory; directories and file data are read from the disk on demand.
// Mounted with MountRW it can also be written (write.go). Safe for
// concurrent use: writes are serialized, reads of directories share a lock.
type FS struct {
	r            io.ReaderAt
	clusterBytes int64
	dataStart    int64 // byte offset of cluster 2
	rootCluster  uint32
	fat          []uint32
	Label        string

	mu       sync.RWMutex
	disk     Disk           // nil: read-only
	fatStart int64          // byte offset of the first FAT
	fatBytes int64          // one FAT's size
	numFATs  int            // copies kept identical
	fsInfo   int64          // byte offset of FSInfo, or 0
	dirty    map[int64]bool // FAT sectors (by index) changed since the last commit
	nextFree uint32         // where the next allocation starts looking
}

// Mount reads the boot sector and the first FAT of the volume behind r.
func Mount(r io.ReaderAt) (*FS, error) {
	b := make([]byte, sectorSize)
	if _, err := r.ReadAt(b, 0); err != nil {
		return nil, fmt.Errorf("fat: boot sector: %w", err)
	}
	if b[sigOffset] != 0x55 || b[sigOffset+1] != 0xAA {
		return nil, errors.New("fat: no boot signature")
	}
	bps := int64(le.Uint16(b[bpbBytsPerSec:]))
	spc := int64(b[bpbSecPerClus])
	rsvd := int64(le.Uint16(b[bpbRsvdSecCnt:]))
	nfats := int64(b[bpbNumFATs])
	fatSz := int64(le.Uint32(b[bpbFATSz32:]))
	total := int64(le.Uint32(b[bpbTotSec32:]))
	switch {
	case bps < 512 || bps&(bps-1) != 0 || spc == 0 || spc&(spc-1) != 0 || nfats == 0:
		return nil, errors.New("fat: implausible BPB")
	case le.Uint16(b[bpbFATSz16:]) != 0 || le.Uint16(b[bpbRootEntCnt:]) != 0 || fatSz == 0:
		return nil, errors.New("fat: not FAT32 (FAT12/16 are not supported)")
	}
	clusters := (total - rsvd - nfats*fatSz) / spc
	if clusters < minClusters {
		return nil, fmt.Errorf("fat: %d clusters is FAT12/16 by definition", clusters)
	}
	raw := make([]byte, min(fatSz*bps, (clusters+2)*4))
	if _, err := r.ReadAt(raw, rsvd*bps); err != nil {
		return nil, fmt.Errorf("fat: reading the FAT: %w", err)
	}
	f := &FS{
		r: r, clusterBytes: spc * bps, dataStart: (rsvd + nfats*fatSz) * bps,
		rootCluster: le.Uint32(b[bpbRootClus:]),
		fat:         make([]uint32, len(raw)/4),
		Label:       strings.TrimSpace(string(b[bsVolLab : bsVolLab+11])),
		fatStart:    rsvd * bps, fatBytes: fatSz * bps, numFATs: int(nfats),
		dirty: map[int64]bool{}, nextFree: 2,
	}
	if fsi := int64(le.Uint16(b[bpbFSInfo:])); fsi > 0 && fsi < rsvd {
		f.fsInfo = fsi * bps
	}
	for i := range f.fat {
		f.fat[i] = le.Uint32(raw[4*i:]) & fatMask
	}
	return f, nil
}

// chain lists a file's clusters, refusing loops and out-of-range links.
func (f *FS) chain(first uint32) ([]uint32, error) {
	var out []uint32
	for c := first; c >= 2 && c < fatBad; c = f.fat[c] {
		if int(c) >= len(f.fat) || len(out) > len(f.fat) {
			return nil, errors.New("fat: broken cluster chain")
		}
		out = append(out, c)
	}
	return out, nil
}

// entry is one directory entry with its long name resolved.
type entry struct {
	name    string
	dir     bool
	first   uint32
	size    int64
	modTime time.Time
}

func (e *entry) info() fs.FileInfo { return fileInfo{e} }

// readDir decodes a directory's entries ("." and ".." left out).
func (f *FS) readDir(first uint32) ([]*entry, error) {
	d, err := f.scan(first)
	if err != nil {
		return nil, err
	}
	out := make([]*entry, 0, len(d.recs))
	for _, r := range d.recs {
		if r.e.name != "." && r.e.name != ".." {
			out = append(out, r.e)
		}
	}
	return out, nil
}

// dirRec is one decoded entry with where it lives: slots [start, end] of
// its directory (the long-name entries, then the short one at end).
type dirRec struct {
	e          *entry
	short      []byte // the 32-byte short entry
	start, end int
}

// dirData is a directory as stored: its raw slots, where each lives on
// the disk, and the decoded entries.
type dirData struct {
	raw  []byte  // every slot of every cluster
	offs []int64 // disk offset of each slot
	recs []dirRec
}

// scan reads and decodes the directory whose first cluster is first.
func (f *FS) scan(first uint32) (*dirData, error) {
	clusters, err := f.chain(first)
	if err != nil {
		return nil, err
	}
	d := &dirData{}
	for _, c := range clusters {
		buf := make([]byte, f.clusterBytes)
		base := f.dataStart + int64(c-2)*f.clusterBytes
		if _, err := f.r.ReadAt(buf, base); err != nil {
			return nil, err
		}
		d.raw = append(d.raw, buf...)
		for off := int64(0); off < f.clusterBytes; off += dirEntry {
			d.offs = append(d.offs, base+off)
		}
	}
	var long []uint16
	var sum byte
	start := -1
	for i := 0; i < len(d.offs); i++ {
		e := d.raw[i*dirEntry : (i+1)*dirEntry]
		switch {
		case e[0] == entryEnd:
			return d, nil
		case e[0] == entryDeleted:
			long, start = nil, -1
		case e[dirAttr] == attrLFN:
			if e[lfnOrd]&lfnLast != 0 {
				long, sum, start = nil, e[lfnChecksum], i
			}
			part := make([]uint16, 0, lfnCharsEach)
			for _, o := range lfnOffsets {
				part = append(part, le.Uint16(e[o:]))
			}
			long = append(part, long...) // parts come last-first
		case e[dirAttr]&attrVolumeID != 0:
			long, start = nil, -1
		default:
			name := shortName(e)
			if long != nil && sum == shortChecksum(e) {
				name = decodeLong(long)
			} else {
				start = i // a short name alone (or orphaned long entries before it)
			}
			if start < 0 {
				start = i
			}
			d.recs = append(d.recs, dirRec{e: &entry{
				name:    name,
				dir:     e[dirAttr]&attrDirectory != 0,
				first:   uint32(le.Uint16(e[dirClusHi:]))<<16 | uint32(le.Uint16(e[dirClusLo:])),
				size:    int64(le.Uint32(e[dirFileSize:])),
				modTime: unpackTime(le.Uint16(e[dirWrtDate:]), le.Uint16(e[dirWrtTime:])),
			}, short: e, start: start, end: i})
			long, start = nil, -1
		}
	}
	return d, nil
}

func decodeLong(u []uint16) string {
	for i, c := range u {
		if c == 0 {
			u = u[:i]
			break
		}
	}
	return string(utf16.Decode(u))
}

// shortName is "NAME.EXT" with the NT lower-case flags applied.
func shortName(e []byte) string {
	base := strings.TrimRight(string(e[0:8]), " ")
	ext := strings.TrimRight(string(e[8:11]), " ")
	if e[dirNTRes]&0x08 != 0 {
		base = strings.ToLower(base)
	}
	if e[dirNTRes]&0x10 != 0 {
		ext = strings.ToLower(ext)
	}
	if ext == "" {
		return base
	}
	return base + "." + ext
}

func unpackTime(date, tod uint16) time.Time {
	return time.Date(int(date>>9)+1980, time.Month(date>>5&0xF), int(date&0x1F),
		int(tod>>11), int(tod>>5&0x3F), int(tod&0x1F)*2, 0, time.UTC)
}

// lookup walks name (an io/fs path) from the root.
func (f *FS) lookup(name string) (*entry, error) {
	cur := &entry{name: ".", dir: true, first: f.rootCluster}
	if name == "." {
		return cur, nil
	}
	for _, part := range strings.Split(name, "/") {
		if !cur.dir {
			return nil, fs.ErrNotExist
		}
		ents, err := f.readDir(cur.first)
		if err != nil {
			return nil, err
		}
		var next *entry
		for _, e := range ents {
			if strings.EqualFold(e.name, part) { // FAT names are case-insensitive
				next = e
				break
			}
		}
		if next == nil {
			return nil, fs.ErrNotExist
		}
		cur = next
	}
	return cur, nil
}

// Open implements fs.FS.
func (f *FS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	e, err := f.lookup(name)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	if e.dir {
		ents, err := f.readDir(e.first)
		if err != nil {
			return nil, &fs.PathError{Op: "open", Path: name, Err: err}
		}
		return &dir{e: e, ents: ents}, nil
	}
	clusters, err := f.chain(e.first)
	if err != nil || int64(len(clusters))*f.clusterBytes < e.size {
		return nil, &fs.PathError{Op: "open", Path: name, Err: errors.New("fat: file shorter than its size")}
	}
	return &file{fsys: f, e: e, clusters: clusters}, nil
}
