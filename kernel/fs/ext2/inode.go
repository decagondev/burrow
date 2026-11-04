package ext2

// inode.go - inodes (W19-S01): reading one by number, symbolic link
// targets (fast ones in the block pointers, slow ones in a data block), and
// the fs.FileInfo built from an inode, whose Sys() is a vfs.Attr. Never
// trusts an inode number or a size without checking it against the volume.
// Docs: WAVE-19-unix.md.

import (
	"fmt"
	"io/fs"
	"time"

	"burrow/kernel/vfs"
)

type inode struct {
	ino          uint32
	mode         uint16
	uid, gid     uint32
	size         int64
	links        uint16
	blocks512    uint32 // storage used, in 512-byte units
	fileACL      uint32 // an extended attribute block, counted in blocks512
	ptr          [nPointers]uint32
	raw          [nPointers * 4]byte // the pointers as stored: a fast link's target
	atime, ctime uint32
	mtime        uint32
}

func (in *inode) isDir() bool  { return in.mode&modeType == modeDir }
func (in *inode) isLink() bool { return in.mode&modeType == modeLink }
func (in *inode) isReg() bool  { return in.mode&modeType == modeReg }

// inode reads inode number ino (they count from 1).
func (f *FS) inode(ino uint32) (*inode, error) {
	if ino == 0 || ino > f.sb.inodes {
		return nil, fmt.Errorf("inode %d out of range", ino)
	}
	g, idx := (ino-1)/f.sb.inodesPerGroup, (ino-1)%f.sb.inodesPerGroup
	b := make([]byte, oldInodeSize)
	if _, err := f.r.ReadAt(b, f.tables[g]+int64(idx)*f.sb.inodeSize); err != nil {
		return nil, fmt.Errorf("inode %d: %w", ino, err)
	}
	in := &inode{
		ino: ino, mode: le.Uint16(b[iMode:]),
		uid:  uint32(le.Uint16(b[iUID:])) | uint32(le.Uint16(b[iUIDHigh:]))<<16,
		gid:  uint32(le.Uint16(b[iGID:])) | uint32(le.Uint16(b[iGIDHigh:]))<<16,
		size: int64(le.Uint32(b[iSize:])), links: le.Uint16(b[iLinks:]),
		blocks512: le.Uint32(b[iBlocks:]), fileACL: le.Uint32(b[iFileACL:]),
		atime: le.Uint32(b[iAtime:]), ctime: le.Uint32(b[iCtime:]), mtime: le.Uint32(b[iMtime:]),
	}
	if in.isReg() && f.sb.roCompat&roCompatLargeFile != 0 { // directories keep i_dir_acl there
		in.size |= int64(le.Uint32(b[iSizeHigh:])) << 32
	}
	copy(in.raw[:], b[iBlock:iBlock+len(in.raw)])
	for i := range in.ptr {
		in.ptr[i] = le.Uint32(in.raw[4*i:])
	}
	if in.size < 0 {
		return nil, fmt.Errorf("inode %d: size %d", ino, in.size)
	}
	return in, nil
}

// linkTarget reads a symbolic link's target. A fast link (shorter than the
// pointers and with no data block) is the pointers' bytes.
func (f *FS) linkTarget(in *inode) (string, error) {
	data := in.blocks512
	if in.fileACL != 0 {
		data -= uint32(f.sb.blockSize / 512)
	}
	if in.size < fastLinkMax && data == 0 {
		return string(in.raw[:in.size]), nil
	}
	if in.size > f.sb.blockSize {
		return "", fmt.Errorf("inode %d: a link target of %d bytes", in.ino, in.size)
	}
	b := make([]byte, in.size)
	if _, err := newBlockMap(f, in).readAt(b, 0); err != nil {
		return "", fmt.Errorf("inode %d: link target: %w", in.ino, err)
	}
	return string(b), nil
}

// fileInfo describes one inode under one name.
type fileInfo struct {
	name string
	in   *inode
}

func (f *FS) info(name string, in *inode) fileInfo { return fileInfo{name, in} }

func (i fileInfo) Name() string       { return i.name }
func (i fileInfo) Size() int64        { return i.in.size }
func (i fileInfo) IsDir() bool        { return i.in.isDir() }
func (i fileInfo) ModTime() time.Time { return unix(i.in.mtime) }
func (i fileInfo) Mode() fs.FileMode  { return fileMode(i.in.mode) }

func (i fileInfo) Sys() any {
	return vfs.Attr{UID: i.in.uid, GID: i.in.gid, Nlink: uint32(i.in.links), Ino: uint64(i.in.ino),
		Atime: unix(i.in.atime), Ctime: unix(i.in.ctime)}
}

// unix is an inode time: seconds since 1970, signed 32 bits (ext2's 128-byte
// inode has nothing finer).
func unix(t uint32) time.Time { return time.Unix(int64(int32(t)), 0).UTC() }

// fileMode is i_mode as an fs.FileMode.
func fileMode(m uint16) fs.FileMode {
	mode := fs.FileMode(m & 0o777)
	switch m & modeType {
	case modeDir:
		mode |= fs.ModeDir
	case modeLink:
		mode |= fs.ModeSymlink
	case modeChr:
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case modeBlk:
		mode |= fs.ModeDevice
	case modeFifo:
		mode |= fs.ModeNamedPipe
	case modeSock:
		mode |= fs.ModeSocket
	}
	if m&modeSetuid != 0 {
		mode |= fs.ModeSetuid
	}
	if m&modeSetgid != 0 {
		mode |= fs.ModeSetgid
	}
	if m&modeSticky != 0 {
		mode |= fs.ModeSticky
	}
	return mode
}
