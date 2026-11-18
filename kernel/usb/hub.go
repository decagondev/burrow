// hub.go is the hub class driver (W28-S02; USB 2.0 §11.23-11.24): a hub
// interface (class 09) is asked for its hub descriptor, its host
// controller is told it is a hub (HubHost.MakeHub: its port count and
// transaction translator), every port is powered, each connected port is
// reset and its device handed to the host controller to address
// (HubHost.Attach) and bound through Connect, so a hub behind a hub works
// the same way, up to the five tiers USB allows. Then a goroutine reads the
// hub's status-change endpoint: a device plugged in later is enumerated,
// one pulled out is detached (its pipes fail, its slot is freed). A USB 3
// hub's SuperSpeed half is reported and not driven: its USB 2 half, a hub
// of its own on the same cable, carries keyboards and mice. Port settle
// times are real milliseconds (PIT-105). Never touches a controller.
package usb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"burrow/kernel/driver"
)

// Hub class codes, requests, features and port status bits (USB 2.0
// §11.24.2, Tables 11-17 and 11-21).
const (
	ClassHub = 9

	descHub    = 0x29
	reqTypeHub = TypeClass              // to the hub
	reqTypePrt = TypeClass | recipOther // to one of its ports
	recipOther = 0x03                   // bmRequestType recipient "other"
	reqGetStat = 0x00                   // GET_STATUS
	reqClrFeat = 0x01                   // CLEAR_FEATURE
	reqSetFeat = 0x03                   // SET_FEATURE
	portStatus = 4                      // wPortStatus + wPortChange
	maxTiers   = 5                      // hubs between the root and a device
	hubDescMax = 71                     // the longest a hub descriptor gets

	featPortReset  = 4
	featPortPower  = 8
	featCConnect   = 16
	featCEnable    = 17
	featCSuspend   = 18
	featCOverCurr  = 19
	featCPortReset = 20

	stConnection = 1 << 0
	stEnable     = 1 << 1
	stReset      = 1 << 4
	stLowSpeed   = 1 << 9
	stHighSpeed  = 1 << 10

	chConnection = 1 << 0
	chReset      = 1 << 4
)

var (
	hubResetWait    = 500 * time.Millisecond // a port reset is 10-20 ms (§7.1.7.5)
	hubResetRecover = 10 * time.Millisecond  // TRSTRCY
	hubPoll         = 5 * time.Millisecond
	errNoHost       = errors.New("its host controller cannot address devices behind it")
)

// HubDescriptor is the parts of a hub descriptor the driver uses.
type HubDescriptor struct {
	Ports          int
	ThinkTime      int // 0-3 (wHubCharacteristics bits 6:5)
	PowerGood      time.Duration
	Compound       bool
	Characteristic uint16
}

// ParseHub reads a USB 2 hub descriptor.
func ParseHub(b []byte) (HubDescriptor, error) {
	if len(b) < 7 || b[1] != descHub || int(b[0]) > len(b) {
		return HubDescriptor{}, fmt.Errorf("usb: not a hub descriptor (% x)", b[:min(len(b), 2)])
	}
	c := binary.LittleEndian.Uint16(b[3:])
	h := HubDescriptor{Ports: int(b[2]), Characteristic: c, ThinkTime: int(c>>5) & 3,
		PowerGood: time.Duration(b[5]) * 2 * time.Millisecond, Compound: c&(1<<2) != 0}
	return h, nil
}

// hub is one started hub.
type hub struct {
	env   *driver.Env
	d     Device
	host  HubHost
	desc  HubDescriptor
	mu    sync.Mutex
	child map[int]bool // ports with a device addressed
}

// startHub runs the hub on ifc of d. It returns what to report.
func startHub(env *driver.Env, d Device, ifc Interface) (string, error) {
	host, ok := d.(HubHost)
	if !ok {
		return "hub", errNoHost
	}
	if host.LinkSpeed() == SpeedSuper {
		return "hub (its SuperSpeed half: not driven; devices are on its USB 2 half)", nil
	}
	if host.Depth()+1 > maxTiers {
		return "hub", fmt.Errorf("more than %d tiers of hubs", maxTiers)
	}
	buf := make([]byte, hubDescMax)
	n, err := d.Control(Setup{RequestType: DirIn | reqTypeHub, Request: ReqGetDescriptor,
		Value: descHub << 8, Length: uint16(len(buf))}, buf)
	if err != nil {
		return "hub", fmt.Errorf("hub descriptor: %w", err)
	}
	desc, err := ParseHub(buf[:n])
	if err != nil {
		return "hub", err
	}
	// One TT for every port: a multi-TT hub (interface protocol 2) runs
	// as a single-TT one until alternate setting 1 is selected, never here.
	if err := host.MakeHub(desc.Ports, desc.ThinkTime, false); err != nil {
		return "hub", fmt.Errorf("make hub: %w", err)
	}
	h := &hub{env: env, d: d, host: host, desc: desc, child: map[int]bool{}}
	for p := 1; p <= desc.Ports; p++ {
		if err := h.feature(reqSetFeat, featPortPower, p); err != nil {
			return "hub", fmt.Errorf("port %d power: %w", p, err)
		}
	}
	time.Sleep(max(desc.PowerGood, 20*time.Millisecond))
	for p := 1; p <= desc.Ports; p++ {
		h.check(p)
	}
	if ep, ok := interruptIn(ifc); ok {
		if pipe, err := d.Open(ep); err == nil {
			go h.watch(pipe, max(int(ep.MaxPacket), (desc.Ports+8)/8))
		} else {
			env.Log.Warnf("hub %s: status change endpoint: %v; no hot-plug", host.Path(), err)
		}
	}
	return fmt.Sprintf("hub, %d ports", desc.Ports), nil
}

// feature sends SET_FEATURE or CLEAR_FEATURE for one port.
func (h *hub) feature(req uint8, feat uint16, port int) error {
	_, err := h.d.Control(Setup{RequestType: reqTypePrt, Request: req, Value: feat, Index: uint16(port)}, nil)
	return err
}

// status reads a port's wPortStatus and wPortChange.
func (h *hub) status(port int) (st, ch uint16, err error) {
	b := make([]byte, portStatus)
	if _, err = h.d.Control(Setup{RequestType: DirIn | reqTypePrt, Request: reqGetStat,
		Index: uint16(port), Length: portStatus}, b); err != nil {
		return 0, 0, err
	}
	return binary.LittleEndian.Uint16(b), binary.LittleEndian.Uint16(b[2:]), nil
}

func (h *hub) path(port int) string { return fmt.Sprintf("%s.%d", h.host.Path(), port) }

// check brings port in line with what is on it: a new device is reset and
// attached, a gone one detached. Changes are acknowledged first.
func (h *hub) check(port int) {
	st, ch, err := h.status(port)
	if err != nil {
		h.env.Log.Warnf("hub %s: port %d status: %v", h.host.Path(), port, err)
		return
	}
	for _, f := range []uint16{featCConnect, featCEnable, featCSuspend, featCOverCurr, featCPortReset} {
		if ch&(1<<(f-featCConnect)) != 0 {
			h.feature(reqClrFeat, f, port)
		}
	}
	h.mu.Lock()
	had := h.child[port]
	h.mu.Unlock()
	if had && (st&stConnection == 0 || ch&chConnection != 0) {
		h.host.Detach(port)
		h.mu.Lock()
		delete(h.child, port)
		h.mu.Unlock()
		Disconnected(h.env, h.path(port))
	}
	if st&stConnection == 0 {
		return
	}
	h.mu.Lock()
	present := h.child[port]
	h.mu.Unlock()
	if present {
		return
	}
	speed, err := h.reset(port)
	if err == nil {
		var a Attached
		if a, err = h.host.Attach(port, speed); err == nil {
			h.mu.Lock()
			h.child[port] = true
			h.mu.Unlock()
			Connect(h.env, a)
			return
		}
	}
	NotConnected(h.env, h.path(port), err)
}

// reset resets port and returns its device's speed once it is enabled.
func (h *hub) reset(port int) (Speed, error) {
	if err := h.feature(reqSetFeat, featPortReset, port); err != nil {
		return 0, fmt.Errorf("reset: %w", err)
	}
	for start := time.Now(); ; time.Sleep(hubPoll) {
		st, ch, err := h.status(port)
		if err != nil {
			return 0, err
		}
		if ch&chReset != 0 || (st&stReset == 0 && st&stEnable != 0) {
			h.feature(reqClrFeat, featCPortReset, port)
			if st&stEnable == 0 {
				return 0, fmt.Errorf("not enabled after reset (status %#x)", st)
			}
			time.Sleep(hubResetRecover)
			return PortSpeed(st), nil
		}
		if time.Since(start) > hubResetWait {
			return 0, fmt.Errorf("reset did not finish (status %#x change %#x)", st, ch)
		}
	}
}

// PortSpeed is the speed a USB 2 hub's port status reports.
func PortSpeed(st uint16) Speed {
	switch {
	case st&stLowSpeed != 0:
		return SpeedLow
	case st&stHighSpeed != 0:
		return SpeedHigh
	}
	return SpeedFull
}

// watch reads the status-change bitmap (bit 0: the hub, bit n: port n)
// and checks each port it names, until the hub goes away.
func (h *hub) watch(p Pipe, size int) {
	buf := make([]byte, size)
	for {
		n, err := p.Transfer(buf)
		if err != nil {
			h.env.Log.Infof("hub %s: %v; its ports are not watched now", h.host.Path(), err)
			return
		}
		for port := 1; port <= h.desc.Ports && port/8 < n; port++ {
			if buf[port/8]&(1<<(port%8)) != 0 {
				h.check(port)
			}
		}
	}
}
