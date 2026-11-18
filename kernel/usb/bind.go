// bind.go starts class drivers for an enumerated device (W27-S03): it sets
// the first configuration and, for every interface a driver here knows,
// starts that driver. HID boot keyboards feed driver.Env's keyboard path
// (Scancodes first, then the console's input through drivers/ps2's
// Decoder), boot mice driver.Env.Mouse; a mass storage interface becomes a
// block device named uba, ubb, ... (W27-S04, storage.go), a hub runs the
// hub driver, which enumerates and binds what is behind it (W28-S02,
// hub.go). Each interface's reports are read
// by a goroutine of its own that ends when its pipe fails (the device is
// gone). Never blocks the caller on a report.
package usb

import (
	"fmt"
	"sync"

	"burrow/kernel/block"
	"burrow/kernel/driver"
	"burrow/kernel/drivers/ps2"
)

// HID interface codes (HID 1.11 §4.2, §4.3).
const (
	ClassHID        = 3
	subclassBoot    = 1
	protocolKbd     = 1
	protocolMouse   = 2
	hidProtocolBoot = 0
)

// Bound is what Bind started on one interface.
type Bound struct {
	Interface int
	What      string // "keyboard", "mouse"
}

// Bind sets cfg and starts a driver on each interface it knows; the rest
// are reported as Bound with What "class C.S.P (no driver)".
func Bind(env *driver.Env, d Device, cfg Config) ([]Bound, error) {
	if err := SetConfiguration(d, cfg.Value); err != nil {
		return nil, fmt.Errorf("set configuration %d: %w", cfg.Value, err)
	}
	var out []Bound
	for _, ifc := range cfg.Interfaces {
		what, err := bindInterface(env, d, ifc)
		if err != nil {
			what = fmt.Sprintf("%s failed: %v", what, err)
		}
		out = append(out, Bound{int(ifc.Number), what})
	}
	return out, nil
}

func bindInterface(env *driver.Env, d Device, ifc Interface) (string, error) {
	switch {
	case ifc.Class == ClassHID && ifc.SubClass == subclassBoot && ifc.Protocol == protocolKbd:
		return "keyboard", startHID(env, d, ifc, keyboardLoop(env))
	case ifc.Class == ClassHID && ifc.SubClass == subclassBoot && ifc.Protocol == protocolMouse:
		return "mouse", startHID(env, d, ifc, mouseLoop(env))
	case ifc.Class == ClassStorage && ifc.SubClass == subclassSCSI && ifc.Protocol == protocolBOT:
		what, err := startStorage(env, d, ifc)
		return "storage" + what, err
	case ifc.Class == ClassHub:
		return startHub(env, d, ifc)
	}
	return fmt.Sprintf("class %02x.%02x.%02x (no driver)", ifc.Class, ifc.SubClass, ifc.Protocol), nil
}

// interruptIn is the interface's first interrupt IN endpoint.
func interruptIn(ifc Interface) (Endpoint, bool) {
	for _, ep := range ifc.Endpoints {
		if ep.In() && ep.Type() == XferInterrupt {
			return ep, true
		}
	}
	return Endpoint{}, false
}

// startHID puts the interface in the boot protocol, reports only on change
// (SET_IDLE 0; a device may refuse it), opens its interrupt endpoint and
// reads reports into each.
func startHID(env *driver.Env, d Device, ifc Interface, each func([]byte)) error {
	ep, ok := interruptIn(ifc)
	if !ok {
		return fmt.Errorf("no interrupt IN endpoint")
	}
	class := uint8(TypeClass | RecipInterface)
	if _, err := d.Control(Setup{RequestType: class, Request: ReqSetProtocol, Value: hidProtocolBoot,
		Index: uint16(ifc.Number)}, nil); err != nil {
		return fmt.Errorf("set boot protocol: %w", err)
	}
	d.Control(Setup{RequestType: class, Request: ReqSetIdle, Index: uint16(ifc.Number)}, nil)
	p, err := d.Open(ep)
	if err != nil {
		return err
	}
	size := max(int(ep.MaxPacket), 8)
	go func() {
		buf := make([]byte, size)
		for {
			n, err := p.Transfer(buf)
			if err != nil {
				env.Log.Warnf("interface %d: %v; reports stop", ifc.Number, err)
				return
			}
			each(buf[:n])
		}
	}()
	return nil
}

func keyboardLoop(env *driver.Env) func([]byte) {
	var kb Keyboard
	var dec ps2.Decoder
	return func(r []byte) {
		for _, sc := range kb.Feed(r) {
			grabbed := env.Scancodes != nil && env.Scancodes(sc)
			for _, ch := range dec.Feed(sc) { // always, so modifiers stay right across a grab
				if env.Input != nil && !grabbed {
					env.Input <- ch
				}
			}
		}
	}
}

func mouseLoop(env *driver.Env) func([]byte) {
	return func(r []byte) {
		if dx, dy, w, b, ok := MouseReport(r); ok && env.Mouse != nil {
			env.Mouse(dx, dy, w, b)
		}
	}
}

var (
	disksMu sync.Mutex
	disks   int // ub disks named so far
)

// startStorage opens LUN 0 and registers it as the next ub disk.
func startStorage(env *driver.Env, d Device, ifc Interface) (string, error) {
	k, err := OpenStorage(d, ifc)
	if err != nil {
		return "", err
	}
	disksMu.Lock()
	name := fmt.Sprintf("ub%c", 'a'+disks)
	disks++
	disksMu.Unlock()
	block.Register(name, k)
	mib := k.Sectors() * uint64(k.SectorSize()) >> 20
	env.Log.Infof("%s: %d MiB in %d-byte blocks, %q", name, mib, k.SectorSize(), k.Model)
	return " " + name, nil
}
