package ps2

// mouse.go - the mouse on the 8042's second port (W14-S02): bring-up
// (enable port 2, set defaults, the IntelliMouse handshake that turns on the
// wheel, enable reporting) and a decoder for its 3- or 4-byte packets.
// Commands to the mouse go through 0xD4 ("next data byte is for port 2").
// Reference: the OSDev wiki "PS/2 Mouse"; Linux drivers/input/mouse/psmouse.

import "fmt"

const (
	MouseIRQ = 12

	cmdEnablePort2 = 0xA8
	cmdTestPort2   = 0xA9
	cmdToPort2     = 0xD4

	mouseDefaults = 0xF6
	mouseRate     = 0xF3
	mouseGetID    = 0xF2
	mouseEnable   = 0xF4
	idWheel       = 0x03

	pktSync     = 0x08 // bit 3 of the first byte is always set
	pktButtons  = 0x07
	pktXSign    = 0x10
	pktYSign    = 0x20
	pktOverflow = 0xC0
)

// wheelRates is the sample-rate sequence that makes an IntelliMouse
// answer id 3 and send a fourth byte with the wheel.
var wheelRates = []byte{200, 100, 80}

// toMouse sends one byte to the mouse and waits for its acknowledgement.
func (c *Controller) toMouse(b byte) error {
	if err := c.command(cmdToPort2); err != nil {
		return err
	}
	if err := c.write(portData, b); err != nil {
		return err
	}
	if r, err := c.read(); err != nil || r != kbdAck {
		return fmt.Errorf("mouse command %#x: %#x, %v", b, r, err)
	}
	return nil
}

// InitMouse brings up port 2 and the mouse on it, with IRQ 12 (call after
// Init). wheel: it answered the IntelliMouse handshake. An error means no
// mouse; the keyboard keeps working.
func (c *Controller) InitMouse() (wheel bool, err error) {
	if r, err := c.query(cmdTestPort2); err != nil || r != portTestOK {
		return false, fmt.Errorf("port 2 test: %#x, %v", r, err)
	}
	if err := c.command(cmdEnablePort2); err != nil {
		return false, err
	}
	if err := c.toMouse(mouseDefaults); err != nil {
		return false, err
	}
	for _, rate := range wheelRates {
		if err := c.toMouse(mouseRate); err != nil {
			return false, err
		}
		if err := c.toMouse(rate); err != nil {
			return false, err
		}
	}
	if err := c.toMouse(mouseGetID); err != nil {
		return false, err
	}
	id, err := c.read()
	if err != nil {
		return false, fmt.Errorf("mouse id: %w", err)
	}
	if err := c.toMouse(mouseEnable); err != nil {
		return false, err
	}
	cfg, err := c.query(cmdReadConfig)
	if err != nil {
		return false, err
	}
	return id == idWheel, c.setConfig(cfg | cfgIRQ12)
}

// MouseDecoder turns mouse bytes into moves in screen directions (dx > 0
// right, dy > 0 down, wheel > 0 up). Bytes out of step (a first byte
// without bit 3, or a packet with an overflow bit) are dropped until the
// stream is back in step.
type MouseDecoder struct {
	Wheel bool // 4-byte packets
	pkt   [4]byte
	n     int
}

// Feed takes one byte; ok when it completed a packet.
func (d *MouseDecoder) Feed(b byte) (dx, dy, wheel int, buttons uint8, ok bool) {
	if d.n == 0 && b&pktSync == 0 {
		return 0, 0, 0, 0, false // not a first byte: resynchronise
	}
	d.pkt[d.n] = b
	d.n++
	size := 3
	if d.Wheel {
		size = 4
	}
	if d.n < size {
		return 0, 0, 0, 0, false
	}
	d.n = 0
	p := d.pkt
	if p[0]&pktOverflow != 0 {
		return 0, 0, 0, p[0] & pktButtons, true // a move too fast to trust: buttons only
	}
	dx = int(p[1])
	if p[0]&pktXSign != 0 {
		dx -= 256
	}
	dy = int(p[2])
	if p[0]&pktYSign != 0 {
		dy -= 256
	}
	if d.Wheel {
		wheel = -int(int8(p[3]<<4) >> 4) // low 4 bits, signed; negative is up
	}
	return dx, -dy, wheel, p[0] & pktButtons, true
}
