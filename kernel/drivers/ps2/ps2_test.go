package ps2

import (
	"io"
	"strings"
	"testing"
	"time"

	"burrow/kernel/driver"
	"burrow/kernel/hal"
	"burrow/kernel/haltest"
	"burrow/kernel/klog"
)

func TestInitProgramsControllerAndKeyboard(t *testing.T) {
	f := haltest.NewFake8042()
	f.Send(false, 0x99) // a stale byte from the BIOS must be flushed
	if err := NewController(f).Init(); err != nil {
		t.Fatal(err)
	}
	cfg, port1, scanning, waiting := f.State()
	if cfg != cfgIRQ1|cfgTranslation {
		t.Errorf("config = %#x, want IRQ 1 and translation only (%#x)", cfg, cfgIRQ1|cfgTranslation)
	}
	if !port1 || !scanning || waiting != 0 {
		t.Errorf("port1=%v scanning=%v leftover=%d", port1, scanning, waiting)
	}
}

func TestInitReportsFailedSelfTest(t *testing.T) {
	f := haltest.NewFake8042()
	f.SelfTest = 0xFC
	if err := NewController(f).Init(); err == nil || !strings.Contains(err.Error(), "self-test") {
		t.Fatalf("Init = %v, want a self-test error", err)
	}
}

// The whole driver: IRQ 1 fires, the Serve goroutine drains the controller
// through the decoder into Input; mouse bytes never reach the console.
func TestKeyboardTypesIntoInput(t *testing.T) {
	f := haltest.NewFake8042()
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
	d := NewDriver()
	if !d.Probe(env) {
		t.Fatal("Probe = false")
	}
	if err := d.Start(env); err != nil {
		t.Fatal(err)
	}
	f.Send(false, 0x23, 0xA3)
	f.Send(true, 0x08, 0x00, 0x00) // a mouse packet
	f.Send(false, 0x17, 0x97, 0x1C, 0x9C)
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
