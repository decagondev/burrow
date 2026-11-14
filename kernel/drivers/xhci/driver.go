// driver.go - the xhci driver's probe and start: every PCI function of
// class 0C.03.30 (an xHCI USB controller), its registers mapped from BAR 0,
// its interrupt chosen (MSI-X where it has it) and bus mastering on before
// the reset; then each connected port's device is enumerated and bound to
// package usb's class drivers, one log line per device, so a real
// machine's report shows what was found and what was not.
package xhci

import (
	"fmt"
	"sync/atomic"
	"unsafe"

	"burrow/kernel/driver"
	"burrow/kernel/drivers/pci"
	"burrow/kernel/usb"
)

const (
	classSerialBus = 0x0C
	subclassUSB    = 0x03
	progIFXHCI     = 0x30
)

func init() { driver.Register(&xhciDriver{}) }

type xhciDriver struct{ devs []pci.Device }

func (*xhciDriver) Name() string { return "xhci" }

func (x *xhciDriver) Probe(env *driver.Env) bool {
	if env.Ports32 == nil || env.MapMMIO == nil || env.DMAPage == nil || env.IRQ == nil || !pci.Present(env.Ports32) {
		return false
	}
	for _, d := range pci.Scan(env.Ports32) {
		if d.Class == classSerialBus && d.Subclass == subclassUSB && d.ProgIF == progIFXHCI {
			x.devs = append(x.devs, d)
		}
	}
	return len(x.devs) > 0
}

func (x *xhciDriver) Start(env *driver.Env) error {
	ok := 0
	for i, dev := range x.devs {
		if err := start(env, dev, i); err != nil {
			env.Log.Warnf("controller %d (%v): %v", i, dev, err)
			continue
		}
		ok++
	}
	if ok == 0 {
		return fmt.Errorf("no usable xHCI controller of %d", len(x.devs))
	}
	return nil
}

type mmio []byte

func (m mmio) R32(off uint32) uint32 { return atomic.LoadUint32((*uint32)(unsafe.Pointer(&m[off]))) }
func (m mmio) W32(off uint32, v uint32) {
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&m[off])), v)
}

// regBytes is how much of BAR 0 the registers span: the ports, the
// runtime registers of interrupter 0 and every slot's doorbell.
func regBytes(caps mmio) uint64 {
	p1 := caps.R32(hcsParams1)
	end := max(caps.R32(capLength)&0xFF+opPorts+portStride*(p1>>24),
		caps.R32(rtsOff)&^0x1F+irERDP+8, caps.R32(dbOff)&^3+4*(p1&0xFF+1))
	return (uint64(end) + pageBytes - 1) &^ (pageBytes - 1)
}

func start(env *driver.Env, dev pci.Device, index int) error {
	base := dev.MemBase64(0)
	if base == 0 {
		return fmt.Errorf("BAR 0 is not memory")
	}
	vec, err := pci.Setup(env, dev, false)
	if err != nil {
		return err
	}
	pci.EnableBusMaster(pci.PortConfig{IO: env.Ports32}, dev)
	caps, err := env.MapMMIO(base, pageBytes)
	if err != nil {
		return fmt.Errorf("map BAR 0 at %#x: %w", base, err)
	}
	mem, err := env.MapMMIO(base, regBytes(mmio(caps)))
	if err != nil {
		return fmt.Errorf("map BAR 0 at %#x: %w", base, err)
	}
	h, err := New(mmio(mem), DMAPage(env.DMAPage))
	if err != nil {
		return err
	}
	h.Warn = env.Log.Warnf
	handler := h.Interrupt
	if vec.Kind != pci.INTx {
		// A message is the controller's alone, and one may arrive for
		// events an earlier call already took: never "nobody claimed".
		handler = func() bool { h.Interrupt(); return true }
	}
	pci.Serve(env, vec, "xhci", handler)
	env.Log.Infof("controller %d: xHCI %x.%02x, %d ports, %d slots, %d-byte contexts, %v",
		index, h.Version>>8, h.Version&0xFF, h.Ports, h.Slots, int(h.ctx), vec)
	for port := 1; port <= h.Ports; port++ {
		if h.Connected(port) {
			attach(env, h, port)
		}
	}
	return nil
}

// attach enumerates the device on port and binds its interfaces.
func attach(env *driver.Env, h *Controller, port int) {
	d, err := h.Enumerate(port)
	if err != nil {
		env.Log.Warnf("port %d: %v", port, err)
		return
	}
	bound, err := usb.Bind(env, d, d.Config)
	what := ""
	for _, b := range bound {
		what += fmt.Sprintf("; interface %d: %s", b.Interface, b.What)
	}
	if err != nil {
		what = "; " + err.Error()
	}
	env.Log.Infof("port %d: %v %04x:%04x %q%s", port, d.Speed, d.Desc.Vendor, d.Desc.Product, d.Product, what)
}
