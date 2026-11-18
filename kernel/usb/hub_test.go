package usb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"burrow/kernel/driver"
	"burrow/kernel/klog"
)

// fakePort is one downstream port of fakeHub.
type fakePort struct {
	status, change uint16
}

// fakeHub plays a USB 2 hub and, as its host controller would, addresses
// what is behind it: Attach hands out a keyboard (kbdConfig).
type fakeHub struct {
	mu       sync.Mutex
	speed    Speed
	ports    []fakePort // index 0 unused
	changes  chan []byte
	made     string
	attached []string
	detached []int
	kbds     []*fakeDevice
}

func newFakeHub(n int) *fakeHub {
	return &fakeHub{speed: SpeedHigh, ports: make([]fakePort, n+1), changes: make(chan []byte, 4)}
}

// plug connects a device of status bits st (speed) on port.
func (f *fakeHub) plug(port int, st uint16) {
	f.mu.Lock()
	f.ports[port] = fakePort{status: stConnection | featPower | st, change: chConnection}
	f.mu.Unlock()
}

func (f *fakeHub) unplug(port int) {
	f.mu.Lock()
	f.ports[port] = fakePort{status: featPower, change: chConnection}
	f.mu.Unlock()
}

const featPower = 1 << 8 // wPortStatus PORT_POWER

func (f *fakeHub) Control(s Setup, data []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case s.Request == ReqGetDescriptor && s.Value == descHub<<8:
		d := []byte{9, descHub, byte(len(f.ports) - 1), 0x40, 0, 50, 0, 0, 0xFF} // think time 2, 100 ms power good
		return copy(data, d), nil
	case s.Request == ReqSetConfiguration:
		return 0, nil
	}
	port := int(s.Index)
	if s.RequestType&0x1F != recipOther || port < 1 || port >= len(f.ports) {
		return 0, ErrStall
	}
	p := &f.ports[port]
	switch {
	case s.Request == reqGetStat:
		binary.LittleEndian.PutUint16(data, p.status)
		binary.LittleEndian.PutUint16(data[2:], p.change)
		return 4, nil
	case s.Request == reqSetFeat && s.Value == featPortPower:
		p.status |= featPower
	case s.Request == reqSetFeat && s.Value == featPortReset:
		if p.status&stConnection != 0 { // the reset is over at once
			p.status |= stEnable
			p.change |= chReset
		}
	case s.Request == reqClrFeat && s.Value >= featCConnect && s.Value <= featCPortReset:
		p.change &^= 1 << (s.Value - featCConnect)
	}
	return 0, nil
}

func (f *fakeHub) Open(ep Endpoint) (Pipe, error) {
	return pipeFunc(func(b []byte) (int, error) {
		c, ok := <-f.changes
		if !ok {
			return 0, errors.New("gone")
		}
		return copy(b, c), nil
	}), nil
}

func (f *fakeHub) MakeHub(ports, think int, multiTT bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.made = fmt.Sprintf("%d ports, think %d, multi %v", ports, think, multiTT)
	return nil
}

func (f *fakeHub) Attach(port int, speed Speed) (Attached, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ports[port].status&stEnable == 0 {
		return Attached{}, errors.New("port not enabled")
	}
	f.attached = append(f.attached, fmt.Sprintf("%d %v", port, speed))
	cfg, _ := ParseConfig(kbdConfig)
	kbd := &fakeDevice{reports: make(chan []byte, 4)}
	f.kbds = append(f.kbds, kbd)
	return Attached{Dev: kbd, Path: fmt.Sprintf("3.%d", port), Speed: speed,
		Desc: DeviceDescriptor{Vendor: 0x0627, Product: 1}, Config: cfg, Product: "Kbd"}, nil
}

func (f *fakeHub) Detach(port int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detached = append(f.detached, port)
}

func (f *fakeHub) Path() string     { return "3" }
func (f *fakeHub) Depth() int       { return 0 }
func (f *fakeHub) LinkSpeed() Speed { return f.speed }
func (f *fakeHub) state() (string, []string, []int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.made, append([]string(nil), f.attached...), append([]int(nil), f.detached...)
}

var hubIfc = Interface{Number: 0, Class: ClassHub, Endpoints: []Endpoint{{Address: 0x81, Attributes: XferInterrupt, MaxPacket: 1}}}

func hubEnv() (*driver.Env, *strings.Builder, *sync.Mutex) {
	var b strings.Builder
	var mu sync.Mutex
	w := writerFunc(func(p []byte) (int, error) { mu.Lock(); defer mu.Unlock(); return b.Write(p) })
	return &driver.Env{Log: klog.New(w, klog.Info), Input: make(chan byte, 8)}, &b, &mu
}

type writerFunc func([]byte) (int, error)

func (w writerFunc) Write(p []byte) (int, error) { return w(p) }

var _ io.Writer = writerFunc(nil)

func TestParseHub(t *testing.T) {
	h, err := ParseHub([]byte{9, descHub, 4, 0x64, 0, 50, 0, 0, 0xFF})
	if err != nil || h.Ports != 4 || h.ThinkTime != 3 || h.PowerGood != 100*time.Millisecond || !h.Compound {
		t.Fatalf("%+v %v", h, err)
	}
	if _, err := ParseHub([]byte{9, DescDevice, 4, 0, 0, 0, 0}); err == nil {
		t.Fatal("a device descriptor taken for a hub's")
	}
}

func TestPortSpeed(t *testing.T) {
	for st, want := range map[uint16]Speed{0: SpeedFull, stLowSpeed: SpeedLow, stHighSpeed: SpeedHigh} {
		if got := PortSpeed(st | stConnection); got != want {
			t.Errorf("status %#x: %v, want %v", st, got, want)
		}
	}
}

// TestHubEnumeratesItsPorts: the hub is described to its host, every port
// powered, and the connected ones reset and attached at their speeds.
func TestHubEnumeratesItsPorts(t *testing.T) {
	hubResetRecover = 0
	f := newFakeHub(4)
	f.plug(2, stLowSpeed)
	f.plug(4, stHighSpeed)
	env, log, mu := hubEnv()
	what, err := startHub(env, f, hubIfc)
	if err != nil || what != "hub, 4 ports" {
		t.Fatalf("%q %v", what, err)
	}
	made, attached, _ := f.state()
	if made != "4 ports, think 2, multi false" {
		t.Errorf("MakeHub: %s", made)
	}
	if strings.Join(attached, ",") != "2 low-speed,4 high-speed" {
		t.Errorf("attached %v", attached)
	}
	for p := 1; p <= 4; p++ {
		if f.ports[p].status&featPower == 0 {
			t.Errorf("port %d not powered", p)
		}
		if f.ports[p].change != 0 {
			t.Errorf("port %d change %#x left set", p, f.ports[p].change)
		}
	}
	mu.Lock()
	out := log.String()
	mu.Unlock()
	if !strings.Contains(out, `port 3.2: low-speed 0627:0001 "Kbd"; interface 0: keyboard`) {
		t.Errorf("log:\n%s", out)
	}
	close(f.changes)
}

// TestHubHotPlug: a device plugged in later is attached when the status
// change endpoint names its port; pulled out, it is detached.
func TestHubHotPlug(t *testing.T) {
	hubResetRecover = 0
	f := newFakeHub(2)
	env, log, mu := hubEnv()
	if _, err := startHub(env, f, hubIfc); err != nil {
		t.Fatal(err)
	}
	f.plug(1, 0)
	f.changes <- []byte{1 << 1}
	waitFor(t, func() bool { _, a, _ := f.state(); return len(a) == 1 && a[0] == "1 full-speed" })
	f.unplug(1)
	f.changes <- []byte{1 << 1}
	waitFor(t, func() bool { _, _, d := f.state(); return len(d) == 1 && d[0] == 1 })
	mu.Lock()
	out := log.String()
	mu.Unlock()
	if !strings.Contains(out, "port 3.1: disconnected") {
		t.Errorf("log:\n%s", out)
	}
	close(f.changes)
}

func TestSuperSpeedHubIsNotDriven(t *testing.T) {
	f := newFakeHub(4)
	f.speed = SpeedSuper
	env, _, _ := hubEnv()
	what, err := startHub(env, f, hubIfc)
	if err != nil || !strings.Contains(what, "not driven") {
		t.Fatalf("%q %v", what, err)
	}
	if made, _, _ := f.state(); made != "" {
		t.Errorf("MakeHub called: %s", made)
	}
}

func TestHubWithoutAHost(t *testing.T) {
	env, _, _ := hubEnv()
	if _, err := startHub(env, &fakeDevice{}, hubIfc); !errors.Is(err, errNoHost) {
		t.Fatalf("%v", err)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
}
