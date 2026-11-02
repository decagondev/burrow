// Package fat reads FAT32 file systems (E07.F06) and writes small ones
// (Format, for cmd/mkfat and the tests). Reading is an io/fs.FS over an
// io.ReaderAt (a block.Device through block.ReaderAt), checked with
// testing/fstest.TestFS on images Format makes. Long file names (VFAT) are
// read and written; FAT12/16 are not supported.
//
// Reference: Microsoft's "FAT: General Overview of On-Disk Format" (the
// FAT32 spec); the offsets below are named after its fields.
package fat

import "encoding/binary"

var le = binary.LittleEndian

const (
	sectorSize = 512
	dirEntry   = 32

	// BPB (boot sector) offsets
	bpbBytsPerSec = 11
	bpbSecPerClus = 13
	bpbRsvdSecCnt = 14
	bpbNumFATs    = 16
	bpbRootEntCnt = 17
	bpbTotSec16   = 19
	bpbMedia      = 21
	bpbFATSz16    = 22
	bpbSecPerTrk  = 24
	bpbNumHeads   = 26
	bpbTotSec32   = 32
	bpbFATSz32    = 36
	bpbRootClus   = 44
	bpbFSInfo     = 48
	bpbBkBootSec  = 50
	bsDrvNum      = 64
	bsBootSig     = 66
	bsVolID       = 67
	bsVolLab      = 71
	bsFilSysType  = 82
	sigOffset     = 510

	// directory entry offsets
	dirName      = 0 // 11 bytes: 8 name + 3 extension, space padded
	dirAttr      = 11
	dirNTRes     = 12 // 0x08 base name lower case, 0x10 extension lower case
	dirClusHi    = 20
	dirWrtTime   = 22
	dirWrtDate   = 24
	dirClusLo    = 26
	dirFileSize  = 28
	lfnOrd       = 0
	lfnChecksum  = 13
	lfnLast      = 0x40
	lfnCharsEach = 13

	attrReadOnly  = 0x01
	attrHidden    = 0x02
	attrSystem    = 0x04
	attrVolumeID  = 0x08
	attrDirectory = 0x10
	attrArchive   = 0x20
	attrLFN       = attrReadOnly | attrHidden | attrSystem | attrVolumeID

	entryEnd     = 0x00
	entryDeleted = 0xE5

	fatMask     = 0x0FFFFFFF
	fatEOC      = 0x0FFFFFF8 // >= is end of chain
	fatBad      = 0x0FFFFFF7
	minClusters = 65525 // fewer is FAT12/16 by definition
)

// lfnOffsets are where a long-name entry keeps its 13 UCS-2 characters.
var lfnOffsets = [lfnCharsEach]int{1, 3, 5, 7, 9, 14, 16, 18, 20, 22, 24, 28, 30}

// shortChecksum is the checksum long-name entries carry of their short name.
func shortChecksum(name []byte) byte {
	var sum byte
	for _, c := range name[:11] {
		sum = (sum&1)<<7 + sum>>1 + c
	}
	return sum
}
