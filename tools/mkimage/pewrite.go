// pewrite.go writes the PE32+ headers and sections ELFToPE (pe.go) laid out:
// a DOS header pointing at the PE signature, the COFF header, the PE32+
// optional header with its 16 data directories, the section table, then each
// section's bytes at a FileAlignment boundary. Timestamps are zero so the
// image is reproducible. Docs: docs/04-plan/waves/WAVE-26-uefi.md.
package main

import "encoding/binary"

func writePE(secs []peSection, entry uint32, relocs bool) []byte {
	le := binary.LittleEndian
	headers := alignUp(uint32(peHeaderOffset+4+20+peOptHeaderSize+40*len(secs)), peFileAlign)
	var sizeCode, sizeData uint32
	fileOff := headers
	offsets := make([]uint32, len(secs))
	for i, s := range secs {
		offsets[i] = fileOff
		raw := alignUp(uint32(len(s.data)), peFileAlign)
		fileOff += raw
		if s.flags&0x20 != 0 {
			sizeCode += raw
		} else {
			sizeData += raw
		}
	}
	last := secs[len(secs)-1]
	sizeImage := alignUp(last.rva+last.vsize, peSectionAlign)
	out := make([]byte, fileOff)

	copy(out, "MZ")
	le.PutUint32(out[0x3C:], peHeaderOffset)
	h := out[peHeaderOffset:]
	copy(h, "PE\x00\x00")
	coff := h[4:]
	le.PutUint16(coff[0:], peMachineAMD64)
	le.PutUint16(coff[2:], uint16(len(secs)))
	le.PutUint16(coff[16:], peOptHeaderSize)
	le.PutUint16(coff[18:], 0x0022) // executable image, large address aware

	opt := coff[20:]
	le.PutUint16(opt[0:], 0x20B) // PE32+
	le.PutUint32(opt[4:], sizeCode)
	le.PutUint32(opt[8:], sizeData)
	le.PutUint32(opt[16:], entry)
	le.PutUint32(opt[20:], secs[0].rva)    // BaseOfCode
	le.PutUint64(opt[24:], 0)              // ImageBase: the firmware relocates
	le.PutUint32(opt[32:], peSectionAlign) // SectionAlignment
	le.PutUint32(opt[36:], peFileAlign)    // FileAlignment
	le.PutUint32(opt[56:], sizeImage)      // SizeOfImage
	le.PutUint32(opt[60:], headers)        // SizeOfHeaders
	le.PutUint16(opt[68:], peSubsystemEFI) // Subsystem
	le.PutUint16(opt[70:], 0x0140)         // DllCharacteristics: dynamic base, NX compatible
	le.PutUint64(opt[72:], 0x10000)        // stack reserve / commit, heap reserve / commit:
	le.PutUint64(opt[80:], 0x10000)        //   unused by firmware, set as linkers do
	le.PutUint64(opt[88:], 0x10000)
	le.PutUint64(opt[96:], 0x1000)
	le.PutUint32(opt[108:], 16) // NumberOfRvaAndSizes
	if relocs {
		r := secs[len(secs)-1]
		le.PutUint32(opt[112+8*peRelocDir:], r.rva)
		le.PutUint32(opt[112+8*peRelocDir+4:], uint32(len(r.data)))
	}

	table := opt[peOptHeaderSize:]
	for i, s := range secs {
		e := table[40*i:]
		copy(e[0:8], s.name)
		le.PutUint32(e[8:], s.vsize)
		le.PutUint32(e[12:], s.rva)
		le.PutUint32(e[16:], alignUp(uint32(len(s.data)), peFileAlign))
		le.PutUint32(e[20:], offsets[i])
		le.PutUint32(e[36:], s.flags)
		copy(out[offsets[i]:], s.data)
	}
	return out
}
