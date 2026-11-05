// Package sound is the kernel's audio policy (W22-S02): /dev/dsp with OSS's
// interface over an Output, which a card's driver (drivers/ac97) provides.
// Pure: no hardware, so every part is tested on the host. format.go turns
// what a program writes (8-bit unsigned or 16-bit signed, mono or stereo)
// into the card's one format, 16-bit signed little-endian stereo; it never
// resamples (the card's rate is set to the program's, VRA).
// Docs: docs/04-plan/waves/WAVE-22-sound-tablet.md; <linux/soundcard.h>.
package sound

// OSS sample formats (<linux/soundcard.h>).
const (
	AFMT_U8     = 0x00000008
	AFMT_S16_LE = 0x00000010

	CardFrameBytes = 4 // 16-bit stereo, what Output takes
)

// Format is what a program writes: its sample format and channels.
type Format struct {
	Fmt      int // AFMT_U8 or AFMT_S16_LE
	Channels int // 1 or 2
}

// FrameBytes is one frame (a sample per channel) in this format.
func (f Format) FrameBytes() int {
	n := 1
	if f.Fmt == AFMT_S16_LE {
		n = 2
	}
	return n * f.Channels
}

// sample16 is the i-th channel sample of a frame at p, as 16-bit signed.
func (f Format) sample16(p []byte, ch int) int16 {
	if f.Channels == 1 {
		ch = 0
	}
	if f.Fmt == AFMT_U8 {
		return int16(int(p[ch])-128) << 8
	}
	return int16(uint16(p[2*ch]) | uint16(p[2*ch+1])<<8)
}

// Convert appends to dst the card's frames for the whole frames in src and
// returns them with the bytes of src it used (a partial frame stays behind).
func (f Format) Convert(dst, src []byte) ([]byte, int) {
	if f == (Format{AFMT_S16_LE, 2}) {
		n := len(src) / CardFrameBytes * CardFrameBytes
		return append(dst, src[:n]...), n
	}
	fb := f.FrameBytes()
	n := len(src) / fb
	for i := 0; i < n; i++ {
		fr := src[i*fb:]
		l, r := f.sample16(fr, 0), f.sample16(fr, 1)
		dst = append(dst, byte(l), byte(uint16(l)>>8), byte(r), byte(uint16(r)>>8))
	}
	return dst, n * fb
}
