package fat

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

// MinImageBytes is the smallest FAT32 image Format makes: one sector per
// cluster needs 65525 clusters plus the FATs.
const MinImageBytes = 34 << 20

// Format writes a FAT32 file system holding the tree of src into img (its
// length is the volume size, at least MinImageBytes). Every name gets a
// long-name entry, so case and length survive; short names are NAME~N.
func Format(img []byte, src fs.FS, label string) error {
	if len(img) < MinImageBytes || len(img)%sectorSize != 0 {
		return fmt.Errorf("fat: image of %d bytes, want at least %d in whole sectors", len(img), MinImageBytes)
	}
	root, err := readTree(src, ".")
	if err != nil {
		return err
	}
	w := newWriter(img)
	w.writeBPB(label)
	w.assign(root, true)
	w.writeTree(root, 0)
	w.writeFSInfo()
	return nil
}

type node struct {
	name     string
	dir      bool
	data     []byte
	mtime    time.Time
	children []*node
	first    uint32 // first cluster (0 = empty file)
}

func readTree(src fs.FS, dir string) (*node, error) {
	n := &node{name: path.Base(dir), dir: true}
	ents, err := fs.ReadDir(src, dir)
	if err != nil {
		return nil, err
	}
	for _, e := range ents {
		p := path.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		var c *node
		switch {
		case e.IsDir():
			if c, err = readTree(src, p); err != nil {
				return nil, err
			}
		case e.Type().IsRegular():
			b, err := fs.ReadFile(src, p)
			if err != nil {
				return nil, err
			}
			c = &node{name: e.Name(), data: b}
		default:
			continue // links and devices have no FAT form
		}
		c.mtime = info.ModTime()
		n.children = append(n.children, c)
	}
	sort.Slice(n.children, func(i, j int) bool { return n.children[i].name < n.children[j].name })
	return n, nil
}

type writer struct {
	img                    []byte
	total, fatSz, rsvd     uint32
	dataStart, nextCluster uint32
	label                  string // 11 characters, space padded
	clusters               uint32
}

func newWriter(img []byte) *writer {
	w := &writer{img: img, total: uint32(len(img) / sectorSize), rsvd: 32, nextCluster: 2}
	w.fatSz = 1
	for {
		w.clusters = w.total - w.rsvd - 2*w.fatSz
		need := ((w.clusters+2)*4 + sectorSize - 1) / sectorSize
		if need <= w.fatSz {
			break
		}
		w.fatSz = need
	}
	w.dataStart = w.rsvd + 2*w.fatSz
	return w
}

func (w *writer) sector(n uint32) []byte { return w.img[n*sectorSize : (n+1)*sectorSize] }

func (w *writer) writeBPB(label string) {
	b := w.sector(0)
	copy(b, []byte{0xEB, 0x58, 0x90})
	copy(b[3:], "BURROW  ")
	le.PutUint16(b[bpbBytsPerSec:], sectorSize)
	b[bpbSecPerClus] = 1
	le.PutUint16(b[bpbRsvdSecCnt:], uint16(w.rsvd))
	b[bpbNumFATs] = 2
	b[bpbMedia] = 0xF8
	le.PutUint16(b[bpbSecPerTrk:], 32)
	le.PutUint16(b[bpbNumHeads:], 64)
	le.PutUint32(b[bpbTotSec32:], w.total)
	le.PutUint32(b[bpbFATSz32:], w.fatSz)
	le.PutUint32(b[bpbRootClus:], 2)
	le.PutUint16(b[bpbFSInfo:], 1)
	le.PutUint16(b[bpbBkBootSec:], 6)
	b[bsDrvNum] = 0x80
	b[bsBootSig] = 0x29
	le.PutUint32(b[bsVolID:], 0x42555252) // "BURR"
	w.label = fmt.Sprintf("%-11.11s", strings.ToUpper(label))
	copy(b[bsVolLab:bsVolLab+11], w.label)
	copy(b[bsFilSysType:], "FAT32   ")
	b[sigOffset], b[sigOffset+1] = 0x55, 0xAA
	copy(w.sector(6), b) // backup boot sector
	w.setFAT(0, 0x0FFFFFF8)
	w.setFAT(1, fatMask)
}

func (w *writer) writeFSInfo() {
	b := w.sector(1)
	le.PutUint32(b[0:], 0x41615252)
	le.PutUint32(b[484:], 0x61417272)
	le.PutUint32(b[488:], w.clusters+2-w.nextCluster) // free clusters
	le.PutUint32(b[492:], w.nextCluster)
	le.PutUint32(b[508:], 0xAA550000)
}

func (w *writer) setFAT(cluster, v uint32) {
	for f := uint32(0); f < 2; f++ {
		off := (w.rsvd+f*w.fatSz)*sectorSize + cluster*4
		le.PutUint32(w.img[off:], v)
	}
}

// alloc takes n consecutive clusters and chains them in the FAT.
func (w *writer) alloc(n int) uint32 {
	first := w.nextCluster
	for i := 0; i < n; i++ {
		c := first + uint32(i)
		if c >= w.clusters+2 {
			panic(errors.New("fat: image full")) // Format checks sizes first
		}
		next := c + 1
		if i == n-1 {
			next = fatMask
		}
		w.setFAT(c, next)
	}
	w.nextCluster += uint32(n)
	return first
}

func (w *writer) clusterBytes(c uint32, n int) []byte {
	start := (w.dataStart + c - 2) * sectorSize
	return w.img[start : start+uint32(n)*sectorSize]
}

func lfnCount(name string) int {
	return (len(utf16.Encode([]rune(name))) + lfnCharsEach - 1) / lfnCharsEach
}

// assign gives every directory and non-empty file its clusters.
func (w *writer) assign(n *node, root bool) {
	if !n.dir {
		if len(n.data) > 0 {
			n.first = w.alloc((len(n.data) + sectorSize - 1) / sectorSize)
		}
		return
	}
	entries := 1 // the root's volume label
	if !root {
		entries = 2 // . and ..
	}
	for _, c := range n.children {
		entries += 1 + lfnCount(c.name)
	}
	n.first = w.alloc(max(1, (entries*dirEntry+sectorSize-1)/sectorSize))
	for _, c := range n.children {
		w.assign(c, false)
	}
}

func (w *writer) writeTree(n *node, parent uint32) {
	if !n.dir {
		if n.first != 0 {
			copy(w.clusterBytes(n.first, (len(n.data)+sectorSize-1)/sectorSize), n.data)
		}
		return
	}
	var ents []byte
	if parent != 0 || n.first != 2 { // not the root
		ents = append(ents, shortEntry(".          ", attrDirectory, n.first, 0, n.mtime)...)
		ents = append(ents, shortEntry("..         ", attrDirectory, parent, 0, n.mtime)...)
	} else {
		// fsck.fat insists the root repeats the boot sector's label (BUG-0011).
		ents = append(ents, shortEntry(w.label, attrVolumeID, 0, 0, n.mtime)...)
	}
	for i, c := range n.children {
		tail := fmt.Sprintf("~%d", i+1) // unique in this directory
		base := shortBase(c.name)
		base = base[:min(len(base), 8-len(tail))]
		short := fmt.Sprintf("%-11s", base+tail)
		attr := byte(attrArchive)
		if c.dir {
			attr = attrDirectory
		}
		ents = append(ents, longEntries(c.name, shortChecksum([]byte(short)))...)
		ents = append(ents, shortEntry(short, attr, c.first, uint32(len(c.data)), c.mtime)...)
	}
	copy(w.img[(w.dataStart+n.first-2)*sectorSize:], ents) // assign sized the chain for these
	for _, c := range n.children {
		p := n.first
		if n.first == 2 {
			p = 0 // ".." of a root child points at cluster 0
		}
		w.writeTree(c, p)
	}
}

// shortBase is the upper-cased, FAT-safe start of a name for its 8.3 alias.
func shortBase(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if r < 0x80 && (r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "F"
	}
	return b.String()
}

func shortEntry(name11 string, attr byte, cluster, size uint32, mtime time.Time) []byte {
	e := make([]byte, dirEntry)
	copy(e[dirName:], name11)
	e[dirAttr] = attr
	le.PutUint16(e[dirClusHi:], uint16(cluster>>16))
	le.PutUint16(e[dirClusLo:], uint16(cluster))
	le.PutUint32(e[dirFileSize:], size)
	d, t := packTime(mtime)
	le.PutUint16(e[dirWrtDate:], d)
	le.PutUint16(e[dirWrtTime:], t)
	return e
}

// longEntries are the VFAT entries for name, last part first as on disk.
func longEntries(name string, sum byte) []byte {
	u := utf16.Encode([]rune(name))
	n := lfnCount(name)
	var out []byte
	for seq := n; seq >= 1; seq-- {
		e := make([]byte, dirEntry)
		e[lfnOrd] = byte(seq)
		if seq == n {
			e[lfnOrd] |= lfnLast
		}
		e[dirAttr] = attrLFN
		e[lfnChecksum] = sum
		for i, off := range lfnOffsets {
			k := (seq-1)*lfnCharsEach + i
			c := uint16(0xFFFF) // padding after the terminator
			switch {
			case k < len(u):
				c = u[k]
			case k == len(u):
				c = 0
			}
			le.PutUint16(e[off:], c)
		}
		out = append(out, e...)
	}
	return out
}

// packTime is FAT's local date and 2-second time; before 1980 is 1980.
func packTime(t time.Time) (date, tod uint16) {
	if t.Year() < 1980 {
		return 1<<5 | 1, 0 // 1980-01-01
	}
	date = uint16(t.Year()-1980)<<9 | uint16(t.Month())<<5 | uint16(t.Day())
	tod = uint16(t.Hour())<<11 | uint16(t.Minute())<<5 | uint16(t.Second()/2)
	return
}
