package usb

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"burrow/kernel/driver"
	"burrow/kernel/klog"
)

// kbdConfig is QEMU's usb-kbd configuration: one HID boot keyboard
// interface (with its HID descriptor between) and an interrupt IN endpoint.
var kbdConfig = []byte{
	9, DescConfig, 34, 0, 1, 1, 0, 0xA0, 50,
	9, DescInterface, 0, 0, 1, ClassHID, 1, 1, 0,
	9, 0x21, 0x11, 0x01, 0, 1, 0x22, 63, 0, // HID descriptor: skipped
	7, DescEndpoint, 0x81, XferInterrupt, 8, 0, 7,
}

func TestParseConfig(t *testing.T) {
	c, err := ParseConfig(kbdConfig)
	if err != nil {
		t.Fatal(err)
	}
	if c.Value != 1 || c.TotalLength != 34 || len(c.Interfaces) != 1 {
		t.Fatalf("config %+v", c)
	}
	ifc := c.Interfaces[0]
	ep, ok := interruptIn(ifc)
	if ifc.Class != ClassHID || ifc.Protocol != protocolKbd || !ok || ep.Number() != 1 || ep.MaxPacket != 8 || ep.Interval != 7 {
		t.Fatalf("interface %+v endpoint %+v", ifc, ep)
	}
	head, err := ParseConfig(kbdConfig[:9])
	if err != nil || head.TotalLength != 34 || len(head.Interfaces) != 0 {
		t.Fatalf("first 9 bytes: %+v %v", head, err)
	}
	bad := append([]byte(nil), kbdConfig...)
	bad[18] = 40 // the HID descriptor claims to run past the end
	if _, err := ParseConfig(bad); err == nil {
		t.Fatal("a descriptor past the end was accepted")
	}
}

func TestParseDevice(t *testing.T) {
	b := []byte{18, DescDevice, 0x00, 0x02, 0, 0, 0, 64, 0x27, 0x06, 0x01, 0x00, 0, 0, 1, 2, 3, 1}
	d, err := ParseDevice(b)
	if err != nil || d.USB != 0x200 || d.MaxPacket0 != 64 || d.Vendor != 0x0627 || d.Product != 1 || d.IProduct != 2 {
		t.Fatalf("%+v %v", d, err)
	}
	if d8, err := ParseDevice(b[:8]); err != errShort || d8.MaxPacket0 != 64 {
		t.Fatalf("8 bytes: %+v %v", d8, err)
	}
}

func report(mods byte, keys ...byte) []byte {
	r := make([]byte, 8)
	r[0] = mods
	copy(r[2:], keys)
	return r
}

func TestKeyboardScancodes(t *testing.T) {
	var k Keyboard
	steps := []struct {
		r    []byte
		want []byte
	}{
		{report(0, 0x04), []byte{0x1E}},                         // a
		{report(0, 0x04, 0x05), []byte{0x30}},                   // and b
		{report(0, 0x05), []byte{0x9E}},                         // a up
		{report(0x02, 0x05), []byte{0x2A}},                      // left shift down
		{report(0x02), []byte{0xB0}},                            // b up
		{report(0), []byte{0xAA}},                               // shift up
		{report(0, 0x52), []byte{0xE0, 0x48}},                   // up arrow
		{report(0), []byte{0xE0, 0xC8}},                         //
		{report(0x10, 0x06), []byte{0xE0, 0x1D, 0x2E}},          // right ctrl, c
		{report(0x10, 1, 1, 1, 1, 1, 1), nil},                   // rollover: ignored
		{report(0), []byte{0xAE, 0xE0, 0x9D}},                   // c up, ctrl up
		{report(0x04, usagePrintScreen), []byte{0x38, scSysRq}}, // Alt+SysRq
		{report(0x04), []byte{scSysRq | 0x80}},                  //
		{report(0, 0x28), []byte{0xB8, 0x1C}},                   // alt up, enter
		{report(0, 0x28)[:4], nil},                              // short: ignored
	}
	for i, s := range steps {
		if got := k.Feed(s.r); !bytes.Equal(got, s.want) {
			t.Fatalf("step %d: % x, want % x", i, got, s.want)
		}
	}
}

func TestMouseReport(t *testing.T) {
	dx, dy, w, b, ok := MouseReport([]byte{5, 0xFE, 3, 0xFF})
	if !ok || dx != -2 || dy != 3 || w != -1 || b != 5 {
		t.Fatal(dx, dy, w, b, ok)
	}
	if _, _, w, _, ok := MouseReport([]byte{0, 1, 1}); !ok || w != 0 {
		t.Fatal("3-byte report")
	}
	if _, _, _, _, ok := MouseReport([]byte{0, 1}); ok {
		t.Fatal("2-byte report accepted")
	}
}

// fakeDevice answers the requests Bind and ReadString make and serves an
// interrupt pipe from a channel of reports.
type fakeDevice struct {
	mu       sync.Mutex
	requests []Setup
	reports  chan []byte
}

func (f *fakeDevice) Control(s Setup, data []byte) (int, error) {
	f.mu.Lock()
	f.requests = append(f.requests, s)
	f.mu.Unlock()
	if s.Request == ReqGetDescriptor && s.Value == DescString<<8|2 {
		return copy(data, []byte{10, DescString, 'K', 0, 'b', 0, 'd', 0, '!', 0}), nil
	}
	if s.Request == ReqSetIdle {
		return 0, ErrStall // allowed: SET_IDLE is optional
	}
	return 0, nil
}

func (f *fakeDevice) Open(ep Endpoint) (Pipe, error) { return pipeFunc(f.read), nil }

func (f *fakeDevice) read(buf []byte) (int, error) {
	r, ok := <-f.reports
	if !ok {
		return 0, errors.New("gone")
	}
	return copy(buf, r), nil
}

type pipeFunc func([]byte) (int, error)

func (p pipeFunc) Transfer(b []byte) (int, error) { return p(b) }

func TestReadString(t *testing.T) {
	f := &fakeDevice{}
	if s := ReadString(f, 2); s != "Kbd!" {
		t.Fatalf("%q", s)
	}
	if s := ReadString(f, 0); s != "" {
		t.Fatalf("index 0: %q", s)
	}
}

func TestBindKeyboard(t *testing.T) {
	cfg, _ := ParseConfig(kbdConfig)
	f := &fakeDevice{reports: make(chan []byte, 4)}
	var scancodes []byte
	var mu sync.Mutex
	input := make(chan byte, 8)
	env := &driver.Env{Log: klog.New(io.Discard, klog.Info), Input: input,
		Scancodes: func(sc byte) bool { mu.Lock(); scancodes = append(scancodes, sc); mu.Unlock(); return false }}
	bound, err := Bind(env, f, cfg)
	if err != nil || len(bound) != 1 || bound[0].What != "keyboard" {
		t.Fatalf("%+v %v", bound, err)
	}
	f.reports <- report(0, 0x0B) // h
	f.reports <- report(0)
	select {
	case ch := <-input:
		if ch != 'h' {
			t.Fatalf("typed %q", ch)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no input")
	}
	close(f.reports)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.requests[0].Request != ReqSetConfiguration || f.requests[0].Value != 1 ||
		f.requests[1].Request != ReqSetProtocol || f.requests[1].Value != hidProtocolBoot {
		t.Fatalf("requests %+v", f.requests)
	}
}
