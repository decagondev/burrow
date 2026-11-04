package ext2

// layout.go - the on-disk format of ext2 (W19-S01): the superblock, group
// descriptors, inodes and directory entries, as byte offsets, and the
// superblock's checks. Every number of the format lives here. Little-endian
// throughout. Never trusts a field before checking it is plausible: the
// volume may come from anywhere. Docs: ADR-0010; the format is described in
// "The Second Extended File System" (Dave Poirier) and e2fsprogs' ext2_fs.h.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

var le = binary.LittleEndian

const (
	superOffset = 1024 // bytes into the volume, whatever the block size
	superSize   = 1024
	magic       = 0xEF53
	rootIno     = 2

	// superblock fields
	sInodesCount     = 0
	sBlocksCount     = 4
	sFreeBlocks      = 12
	sFreeInodes      = 16
	sFirstDataBlock  = 20
	sLogBlockSize    = 24 // block size = 1024 << this
	sBlocksPerGroup  = 32
	sInodesPerGroup  = 40
	sWtime           = 48
	sMagic           = 56
	sRevLevel        = 76
	sFirstIno        = 84
	sInodeSize       = 88
	sFeatureIncompat = 96
	sFeatureROCompat = 100
	sVolumeName      = 120
	volumeNameLen    = 16
	sWantExtraIsize  = 0x15C
	oldFirstIno      = 11 // revision 0's first inode for files
	maxLogBlockSize  = 6  // 64 KiB

	// features this reader understands
	incompatFiletype    = 0x0002 // directory entries carry the file's type
	roCompatSparseSuper = 0x0001 // superblock copies in some groups only
	roCompatLargeFile   = 0x0002 // i_size_high holds a file's upper 32 bits
	knownIncompat       = incompatFiletype
	knownROCompat       = roCompatSparseSuper | roCompatLargeFile

	// group descriptors
	groupDescSize = 32
	bgBlockBitmap = 0
	bgInodeBitmap = 4
	bgInodeTable  = 8
	bgFreeBlocks  = 12 // 16 bits, as the next two
	bgFreeInodes  = 14
	bgUsedDirs    = 16

	// inodes
	oldInodeSize = 128 // revision 0, and the part every revision shares
	iMode        = 0
	iUID         = 2
	iSize        = 4
	iAtime       = 8
	iCtime       = 12
	iMtime       = 16
	iDtime       = 20
	iGID         = 24
	iLinks       = 26
	iBlocks      = 28 // in 512-byte units
	iFlags       = 32
	iBlock       = 40 // 15 block pointers
	iFileACL     = 104
	iSizeHigh    = 108
	iUIDHigh     = 120
	iGIDHigh     = 122
	iExtraIsize  = 128    // where an inode is bigger than revision 0's
	indexFlag    = 0x1000 // EXT2_INDEX_FL: an htree directory
	maxNameLen   = 255
	nPointers    = 15
	nDirect      = 12
	fastLinkMax  = 60 // a shorter link target lives in the pointers themselves

	// i_mode
	modeType   = 0xF000
	modeFifo   = 0x1000
	modeChr    = 0x2000
	modeDir    = 0x4000
	modeBlk    = 0x6000
	modeReg    = 0x8000
	modeLink   = 0xA000
	modeSock   = 0xC000
	modeSetuid = 0x0800
	modeSetgid = 0x0400
	modeSticky = 0x0200

	// directory entries
	deInode    = 0
	deRecLen   = 4
	deNameLen  = 6 // a byte with the filetype feature, else 16 bits
	deFileType = 7
	deName     = 8

	maxLinks = 40 // symbolic links followed in one lookup, as Linux
)

// super is the superblock's geometry, checked.
type super struct {
	blockSize      int64
	blocks         uint32
	firstDataBlock uint32
	blocksPerGroup uint32
	inodesPerGroup uint32
	inodes         uint32
	inodeSize      int64
	firstIno       uint32 // the first inode not reserved
	wantExtra      uint16 // i_extra_isize for new inodes
	groups         uint32
	incompat       uint32
	roCompat       uint32
	label          string
}

func parseSuper(b []byte) (super, error) {
	if le.Uint16(b[sMagic:]) != magic {
		return super{}, errors.New("ext2: not ext2 (no magic number)")
	}
	logBS := le.Uint32(b[sLogBlockSize:])
	if logBS > maxLogBlockSize {
		return super{}, fmt.Errorf("ext2: block size 1024<<%d is implausible", logBS)
	}
	s := super{
		blockSize: 1024 << logBS, blocks: le.Uint32(b[sBlocksCount:]),
		firstDataBlock: le.Uint32(b[sFirstDataBlock:]), blocksPerGroup: le.Uint32(b[sBlocksPerGroup:]),
		inodesPerGroup: le.Uint32(b[sInodesPerGroup:]), inodes: le.Uint32(b[sInodesCount:]),
		inodeSize: oldInodeSize, firstIno: oldFirstIno,
		label: strings.TrimRight(string(b[sVolumeName:sVolumeName+volumeNameLen]), "\x00"),
	}
	if le.Uint32(b[sRevLevel:]) >= 1 {
		s.inodeSize = int64(le.Uint16(b[sInodeSize:]))
		s.firstIno, s.wantExtra = le.Uint32(b[sFirstIno:]), le.Uint16(b[sWantExtraIsize:])
		s.incompat, s.roCompat = le.Uint32(b[sFeatureIncompat:]), le.Uint32(b[sFeatureROCompat:])
	}
	if s.incompat&^knownIncompat != 0 {
		return super{}, fmt.Errorf("ext2: incompatible features %#x (ext3/ext4?)", s.incompat&^knownIncompat)
	}
	if s.blocksPerGroup == 0 || s.inodesPerGroup == 0 || s.blocks <= s.firstDataBlock ||
		s.inodeSize < oldInodeSize || s.inodeSize&(s.inodeSize-1) != 0 || s.inodeSize > s.blockSize {
		return super{}, errors.New("ext2: implausible geometry in the superblock")
	}
	s.groups = (s.blocks - s.firstDataBlock + s.blocksPerGroup - 1) / s.blocksPerGroup
	if uint64(s.groups)*uint64(s.inodesPerGroup) < uint64(s.inodes) {
		return super{}, errors.New("ext2: implausible geometry: more inodes than the groups hold")
	}
	return s, nil
}
