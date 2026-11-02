package ps2

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"burrow/kernel/driver"
	"burrow/kernel/hal"
	"burrow/kernel/haltest"
	"burrow/kernel/klog"
)

// fake8042 answers the controller commands Init uses, the way QEMU's does.
type fake8042 struct {
	mu          sync.Mutex
	out         []byte // waiting in the output buffer
	aux         []bool // per byte: from port 2
	cfg         byte
	wantCfg     bool // the next data byte is a configuration byte
	selfTest    byte
	port1, scan bool
}

func newFake() *fake8042 {
	return &fake8042{cfg: cfgIRQ1 | cfgIRQ12 | cfgTranslation, selfTest: selfTestOK}
}

func (f *fake8042) push(b byte, aux bool) { f.out = append(f.out, b); f.aux = append(f.aux, aux) }

func (f *fake8042) In8(port uint16) uint8 {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch port {
	case portStatus:
		var st byte
		if len(f.out) > 0 {
			st |= stOutputFull
			if f.aux[0] {
				st |= stAux
			}
		}
		return st
	case portData:
		if len(f.out) == 0 {
			return 0
		}
		b := f.out[0]
		f.out, f.aux = f.out[1:], f.aux[1:]
		return b
	}
	return 0xFF
}

func (f *fake8042) Out8(port uint16, v uint8) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if port == portCmd {
		switch v {
		case cmdReadConfig:
			f.push(f.cfg, false)
		case cmdWriteConfig:
			f.wantCfg = true
		case cmdSelfTest:
			f.push(f.selfTest, false)
		case cmdTestPort1:
			f.push(portTestOK, false)
		case cmdEnablePort1:
			f.port1 = true
		case cmdDisablePort1:
			f.port1 = false
		}
		return
	}
	switch {
	case f.wantCfg:
		f.cfg, f.wantCfg = v, false
	case v == kbdEnable:
		f.scan = true
		f.push(kbdAck, false)
	}
}

// press queues bytes as if the keyboard sent them.
func (f *fake8042) press(aux bool, codes ...byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range codes {
		f.push(c, aux)
	}
}

func TestInitProgramsControllerAndKeyboard(t *testing.T) {
	f := newFake()
	f.press(false, 0x99) // a stale byte from the BIOS must be flushed
	if err := NewController(f).Init(); err != nil {
		t.Fatal(err)
	}
	if f.cfg != cfgIRQ1|cfgTranslation {
		t.Errorf("config = %#x, want IRQ 1 and translation only (%#x)", f.cfg, cfgIRQ1|cfgTranslation)
	}
	if !f.port1 || !f.scan || len(f.out) != 0 {
		t.Errorf("port1=%v scanning=%v leftover=% x", f.port1, f.scan, f.out)
	}
}

func TestInitReportsFailedSelfTest(t *testing.T) {
	f := newFake()
	f.selfTest = 0xFC
	if err := NewController(f).Init(); err == nil || !strings.Contains(err.Error(), "self-test") {
		t.Fatalf("Init = %v, want a self-test error", err)
	}
}

// The whole driver: IRQ 1 fires, the Serve goroutine drains the controller
// through the decoder into Input; mouse bytes never reach the console.
func TestKeyboardTypesIntoInput(t *testing.T) {
	f := newFake()
	q := haltest.NewFakeIRQ()
	defer q.Close()
	input := make(chan byte, 16)
	env := &driver.Env{
		Ports: f, Input: input, Log: klog.New(io.Discard, klog.Info),
		IRQ: func(line int) hal.IRQLine {
			if line != IRQ {
				t.Errorf("asked for irq %d", line)
			}
			return q
		},
	}
	if !(&keyboard{}).Probe(env) {
		t.Fatal("Probe = false")
	}
	if err := (&keyboard{}).Start(env); err != nil {
		t.Fatal(err)
	}
	f.press(false, 0x23, 0xA3)
	f.press(true, 0x08, 0x00, 0x00) // a mouse packet
	f.press(false, 0x17, 0x97, 0x1C, 0x9C)
	q.Fire()
	var got []byte
	for len(got) < 3 {
		select {
		case b := <-input:
			got = append(got, b)
		case <-time.After(2 * time.Second):
			t.Fatalf("typed %q so far, want \"hi\\r\"", got)
		}
	}
	if string(got) != "hi\r" {
		t.Fatalf("typed %q, want \"hi\\r\"", got)
	}
}
