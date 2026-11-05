// video.go - the "video" payload (W21-S01): the display mode an image asks
// stage2 to set through VBE. It mirrors struct video_request in
// include/burrow/manifest.h and the VR_* offsets in boot/boot_abi.inc byte
// for byte. Pure: a string in, 16 bytes out; it never opens files.
package main

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

const (
	videoMagic       = "VIDR"
	VideoRequestSize = 16
	VideoPayloadName = "video"
)

// VideoRequest is the mode an image asks for. PitchBytes 0 keeps the mode's
// own scanline; anything else asks VBE 4F06 for a padded one.
type VideoRequest struct {
	Width, Height uint16
	BPP           uint8
	PitchBytes    uint32
}

// ParseVideo reads "WxHxB" or "WxHxB,pitch" (pitch in bytes per scanline).
func ParseVideo(spec string) (VideoRequest, error) {
	mode, pitchStr, hasPitch := strings.Cut(spec, ",")
	parts := strings.Split(mode, "x")
	if len(parts) != 3 {
		return VideoRequest{}, fmt.Errorf("video %q: want WxHxB[,pitch]", spec)
	}
	var n [3]uint64
	for i, p := range parts {
		v, err := strconv.ParseUint(p, 10, 16)
		if err != nil || v == 0 {
			return VideoRequest{}, fmt.Errorf("video %q: bad number %q", spec, p)
		}
		n[i] = v
	}
	v := VideoRequest{Width: uint16(n[0]), Height: uint16(n[1])}
	switch n[2] {
	case 15, 16, 24, 32:
		v.BPP = uint8(n[2])
	default:
		return VideoRequest{}, fmt.Errorf("video %q: bpp must be 15, 16, 24 or 32", spec)
	}
	if hasPitch {
		p, err := strconv.ParseUint(pitchStr, 10, 32)
		if err != nil {
			return VideoRequest{}, fmt.Errorf("video %q: bad pitch %q", spec, pitchStr)
		}
		if p < uint64(v.Width)*uint64((v.BPP+7)/8) {
			return VideoRequest{}, fmt.Errorf("video %q: pitch %d is shorter than a row", spec, p)
		}
		v.PitchBytes = uint32(p)
	}
	return v, nil
}

// MarshalBinary encodes the request as stage2 reads it.
func (v VideoRequest) MarshalBinary() ([]byte, error) {
	b := make([]byte, VideoRequestSize)
	copy(b, videoMagic)
	le := binary.LittleEndian
	le.PutUint16(b[4:], v.Width)
	le.PutUint16(b[6:], v.Height)
	b[8] = v.BPP
	le.PutUint32(b[12:], v.PitchBytes)
	return b, nil
}
