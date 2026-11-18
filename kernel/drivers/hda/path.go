// path.go turns a chosen output path on (spec §7.3.3) and builds stream
// format words (§3.7.1): the function group and every node on the path in
// D0, each node's connection to the next selected (a mixer sums its inputs
// and has its input amplifier for that one unmuted instead), output
// amplifiers unmuted at their 0 dB step (the capability's offset), the pin
// driving out (headphones enabled, EAPD on where the pin has it), and the
// DAC on the stream's tag with the format. Pure: verbs go through Verber.
package hda

import "fmt"

// SetUp powers and unmutes p and gives its DAC stream tag and format fmt.
func (c *Codec) SetUp(v Verber, p Path, tag int, format uint16) error {
	send := func(nid int, verb uint32) error {
		if _, err := v.Verb(c.CAd, nid, verb); err != nil {
			return fmt.Errorf("node %d verb %#x: %w", nid, verb, err)
		}
		return nil
	}
	if err := send(c.AFG, verbSetPower|0); err != nil {
		return err
	}
	for i, nid := range p.Nodes {
		w := c.Widgets[nid]
		if err := send(nid, verbSetPower|0); err != nil {
			return err
		}
		if i < len(p.Index) {
			if err := c.connect(v, send, w, p.Index[i]); err != nil {
				return err
			}
		}
		if w.Caps&capOutAmp != 0 {
			if err := send(nid, verbSetAmp|ampOutLR|c.zeroDB(v, w, paramOutAmpCaps)); err != nil {
				return err
			}
		}
	}
	pin := c.Widgets[p.Nodes[0]]
	ctl := uint32(pinCtlOut)
	if pin.Device() == 2 {
		ctl |= pinCtlHP
	}
	if err := send(pin.NID, verbSetPinCtl|ctl); err != nil {
		return err
	}
	if pin.PinCaps&pinCapEAPD != 0 {
		if err := send(pin.NID, verbSetEAPD|eapdOn); err != nil {
			return err
		}
	}
	return c.SetFormat(v, p, tag, format)
}

// SetFormat gives the path's DAC its stream tag (channel 0) and format.
func (c *Codec) SetFormat(v Verber, p Path, tag int, format uint16) error {
	dac := p.Nodes[len(p.Nodes)-1]
	if _, err := v.Verb(c.CAd, dac, verbSetStream|uint32(tag)<<4); err != nil {
		return err
	}
	_, err := v.Verb(c.CAd, dac, verbSetFormat|uint32(format))
	return err
}

// connect makes w take its input from connection index i.
func (c *Codec) connect(v Verber, send func(int, uint32) error, w Widget, i int) error {
	if w.Type == TypeMixer {
		if w.Caps&capInAmp == 0 {
			return nil
		}
		return send(w.NID, verbSetAmp|ampInLR|uint32(i)<<8|c.zeroDB(v, w, paramInAmpCaps))
	}
	if len(w.Conns) > 1 {
		return send(w.NID, verbSetSelect|uint32(i))
	}
	return nil
}

// zeroDB is an amplifier's 0 dB step: the offset in its capabilities (the
// widget's own, or the function group's when it does not override them).
func (c *Codec) zeroDB(v Verber, w Widget, param uint32) uint32 {
	nid := c.AFG
	if w.Caps&capAmpOverride != 0 {
		nid = w.NID
	}
	caps, err := v.Verb(c.CAd, nid, verbGetParam|param)
	if err != nil {
		return 0
	}
	return caps & 0x7F
}

// Rates a converter may take (PCM parameter bits 0-10) and their format
// words' base, multiplier and divisor bits (spec Table 53).
var rates = []struct {
	hz  int
	bit uint32
	fmt uint16
}{
	{8000, 1 << 0, 0x0500},  // 48 kHz / 6
	{11025, 1 << 1, 0x4300}, // 44.1 kHz / 4
	{16000, 1 << 2, 0x0200}, // 48 kHz / 3
	{22050, 1 << 3, 0x4100}, // 44.1 kHz / 2
	{32000, 1 << 4, 0x0A00}, // 48 kHz * 2 / 3
	{44100, 1 << 5, 0x4000}, // 44.1 kHz
	{48000, 1 << 6, 0x0000}, // 48 kHz
	{88200, 1 << 7, 0x4800}, // 44.1 kHz * 2
	{96000, 1 << 8, 0x0800}, // 48 kHz * 2
}

const (
	fmt16Bits = 1 << 4 // BITS 6:4 = 001
	fmtStereo = 1      // CHAN 3:0 = channels - 1
	defaultHz = 48000
)

// Format is the format word for hz, 16-bit stereo, at the nearest rate the
// converter's PCM parameter allows (48 kHz when it names none), and that
// rate.
func Format(pcm uint32, hz int) (uint16, int) {
	best := -1
	for i, r := range rates {
		if pcm&r.bit == 0 && !(pcm&0x7FF == 0 && r.hz == defaultHz) {
			continue
		}
		if best < 0 || abs(r.hz-hz) < abs(rates[best].hz-hz) {
			best = i
		}
	}
	if best < 0 {
		return fmt16Bits | fmtStereo, defaultHz
	}
	return rates[best].fmt | fmt16Bits | fmtStereo, rates[best].hz
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
