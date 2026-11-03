package haltest

import "sync"

// Fake8042 is a PS/2 controller (ports 0x60/0x64) that answers the
// commands a driver's bring-up uses the way QEMU's does, with a keyboard on
// port 1 and an IntelliMouse on port 2 (W14-S02), and lets a test queue
// bytes as if a keyboard or mouse sent them. It implements hal.PortIO for
// those two ports only.
type Fake8042 struct {
	mu        sync.Mutex
	out       []byte // waiting in the output buffer
	aux       []bool // per byte: from port 2 (mouse)
	cfg       byte
	wantCfg   bool // the next data byte is a configuration byte
	port1     bool
	scanning  bool
	port2     bool
	toMouse   bool    // the next data byte is for the mouse (0xD4)
	rateArg   bool    // the mouse's next byte is a sample rate
	rates     [3]byte // the last three rates the mouse was set to
	reporting bool

	SelfTest byte // the answer to 0xAA; 0x55 means passed
	NoMouse  bool // port 2's test fails, as on a controller without one
}

// 8042 facts the fake needs (the driver has its own copy: drivers/ps2).
const (
	kbcData   uint16 = 0x60
	kbcStatus uint16 = 0x64

	kbcOutFull = 1 << 0
	kbcAux     = 1 << 5

	kbcReadCfg  = 0x20
	kbcWriteCfg = 0x60
	kbcSelfTest = 0xAA
	kbcTestP1   = 0xAB
	kbcOffP1    = 0xAD
	kbcOnP1     = 0xAE
	kbcTestP2   = 0xA9
	kbcOnP2     = 0xA8
	kbcToP2     = 0xD4
	kbdScan     = 0xF4
	kbdAck      = 0xFA
	mouseRate   = 0xF3
	mouseGetID  = 0xF2
)

// NewFake8042 starts like a BIOS left it: both IRQs and translation on.
func NewFake8042() *Fake8042 { return &Fake8042{cfg: 0x43, SelfTest: 0x55} }

func (f *Fake8042) push(b byte, aux bool) { f.out = append(f.out, b); f.aux = append(f.aux, aux) }

func (f *Fake8042) In8(port uint16) uint8 {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch port {
	case kbcStatus:
		var st byte
		if len(f.out) > 0 {
			st |= kbcOutFull
			if f.aux[0] {
				st |= kbcAux
			}
		}
		return st
	case kbcData:
		if len(f.out) == 0 {
			return 0
		}
		b := f.out[0]
		f.out, f.aux = f.out[1:], f.aux[1:]
		return b
	}
	return 0xFF
}

func (f *Fake8042) Out8(port uint16, v uint8) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if port == kbcStatus { // writes to 0x64 are commands
		switch v {
		case kbcReadCfg:
			f.push(f.cfg, false)
		case kbcWriteCfg:
			f.wantCfg = true
		case kbcSelfTest:
			f.push(f.SelfTest, false)
		case kbcTestP1:
			f.push(0x00, false)
		case kbcOnP1:
			f.port1 = true
		case kbcOffP1:
			f.port1 = false
		case kbcTestP2:
			if f.NoMouse {
				f.push(0x01, false)
			} else {
				f.push(0x00, false)
			}
		case kbcOnP2:
			f.port2 = true
		case kbcToP2:
			f.toMouse = true
		}
		return
	}
	switch {
	case f.toMouse:
		f.toMouse = false
		f.mouseByte(v)
	case f.wantCfg:
		f.cfg, f.wantCfg = v, false
	case v == kbdScan:
		f.scanning = true
		f.push(kbdAck, false)
	}
}

// mouseByte answers a byte sent to the mouse like an IntelliMouse (f.mu held).
func (f *Fake8042) mouseByte(v byte) {
	f.push(kbdAck, true)
	switch {
	case f.rateArg:
		f.rateArg = false
		f.rates = [3]byte{f.rates[1], f.rates[2], v}
	case v == mouseRate:
		f.rateArg = true
	case v == mouseGetID:
		id := byte(0)
		if f.rates == [3]byte{200, 100, 80} {
			id = 3
		}
		f.push(id, true)
	case v == kbdScan:
		f.reporting = true
	}
}

// MouseState reports what the driver set up on port 2.
func (f *Fake8042) MouseState() (enabled, wheel, reporting bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.port2, f.rates == [3]byte{200, 100, 80}, f.reporting
}

// Send queues bytes from the keyboard (aux false) or the mouse (aux true).
func (f *Fake8042) Send(aux bool, codes ...byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range codes {
		f.push(c, aux)
	}
}

// State reports what the driver set up.
func (f *Fake8042) State() (config byte, port1, scanning bool, waiting int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cfg, f.port1, f.scanning, len(f.out)
}
