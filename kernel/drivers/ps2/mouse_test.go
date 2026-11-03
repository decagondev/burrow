package ps2

import (
	"io"
	"testing"
	"time"

	"burrow/kernel/driver"
	"burrow/kernel/hal"
	"burrow/kernel/haltest"
	"burrow/kernel/klog"
)

func TestInitMouse(t *testing.T) {
	f := haltest.NewFake8042()
	c := NewController(f)
	if err := c.Init(); err != nil {
		t.Fatal(err)
	}
	wheel, err := c.InitMouse()
	if err != nil || !wheel {
		t.Fatalf("InitMouse = %v, %v; want a wheel", wheel, err)
	}
	enabled, handshake, reporting := f.MouseState()
	cfg, port1, scanning, waiting := f.State()
	if !enabled || !handshake || !reporting || waiting != 0 {
		t.Errorf("port2=%v handshake=%v reporting=%v leftover=%d", enabled, handshake, reporting, waiting)
	}
	if cfg != cfgIRQ1|cfgIRQ12|cfgTranslation || !port1 || !scanning {
		t.Errorf("config %#x port1=%v scanning=%v: the keyboard must stay as Init left it", cfg, port1, scanning)
	}
}

func TestNoMouse(t *testing.T) {
	f := haltest.NewFake8042()
	f.NoMouse = true
	c := NewController(f)
	if err := c.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InitMouse(); err == nil {
		t.Fatal("InitMouse found a mouse on a controller without one")
	}
	if cfg, _, _, _ := f.State(); cfg&cfgIRQ12 != 0 {
		t.Errorf("config %#x: IRQ 12 on without a mouse", cfg)
	}
}

func TestMouseDecoder(t *testing.T) {
	d := MouseDecoder{Wheel: true}
	type move struct {
		dx, dy, wheel int
		buttons       uint8
	}
	var got []move
	feed := func(bs ...byte) {
		for _, b := range bs {
			if dx, dy, w, btn, ok := d.Feed(b); ok {
				got = append(got, move{dx, dy, w, btn})
			}
		}
	}
	feed(0x00)                           // out of step: no sync bit, dropped
	feed(0x08|0x01|0x20, 10, 0xFB, 0x0F) // left, right 10, PS/2 y -5 (down 5), wheel byte -1 (up)
	feed(0x08|0x10, 0xF0, 0x03, 0x01)    // left 16, up 3, wheel down
	feed(0x08|0x40|0x02, 0x55, 0x55, 0)  // x overflow: buttons only
	want := []move{{10, 5, 1, 1}, {-16, -3, -1, 0}, {0, 0, 0, 2}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("packet %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// The whole driver: a packet arriving with IRQ 12 reaches Env.Mouse in
// screen directions, while keyboard bytes still reach Input.
func TestMouseReachesEnv(t *testing.T) {
	f := haltest.NewFake8042()
	q := haltest.NewFakeIRQ()
	defer q.Close()
	type move struct {
		dx, dy, wheel int
		buttons       uint8
	}
	moves := make(chan move, 4)
	input := make(chan byte, 4)
	env := &driver.Env{
		Ports: f, Input: input, Log: klog.New(io.Discard, klog.Info),
		IRQ:   func(int) hal.IRQLine { return q },
		Mouse: func(dx, dy, w int, b uint8) { moves <- move{dx, dy, w, b} },
	}
	d := NewDriver()
	if err := d.Start(env); err != nil {
		t.Fatal(err)
	}
	f.Send(true, 0x08|0x02, 3, 4, 0) // right button, right 3, up 4
	f.Send(false, 0x23, 0xA3)        // h
	q.Fire()
	select {
	case m := <-moves:
		if m != (move{3, -4, 0, 2}) {
			t.Errorf("move %+v", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no mouse packet")
	}
	select {
	case b := <-input:
		if b != 'h' {
			t.Errorf("typed %q", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the keyboard stopped typing")
	}
}
