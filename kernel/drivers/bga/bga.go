// Package bga drives the Bochs/QEMU display adapter ("BGA", PCI 1234:1111,
// QEMU's -vga std) through its "dispi" registers (E07.F04): pick a mode,
// enable the linear framebuffer, map PCI BAR0 and hand a gfx.Framebuffer to
// the kernel. Reference: the OSDev wiki "Bochs VBE Extensions".
package bga

import (
	"errors"
	"fmt"
	"sync"

	"burrow/kernel/driver"
	"burrow/kernel/drivers/pci"
	"burrow/kernel/gfx"
	"burrow/kernel/hal"
)

const (
	Vendor = 0x1234
	Device = 0x1111

	Width, Height, BPP = 1024, 768, 32

	portIndex uint16 = 0x01CE
	portData  uint16 = 0x01CF

	regID        = 0
	regXRes      = 1
	regYRes      = 2
	regBPP       = 3
	regEnable    = 4
	regVirtWidth = 6

	idMin         = 0xB0C0 // any dispi version
	enEnabled     = 0x01
	enLinearFrame = 0x40
)

func read(io hal.PortIO16, reg uint16) uint16 {
	io.Out16(portIndex, reg)
	return io.In16(portData)
}

func write(io hal.PortIO16, reg, v uint16) {
	io.Out16(portIndex, reg)
	io.Out16(portData, v)
}

// SetMode switches to w x h x 32 with the linear framebuffer on and
// returns the pitch in bytes (the virtual width the card settled on).
func SetMode(io hal.PortIO16, w, h int) (int, error) {
	if id := read(io, regID); id < idMin {
		return 0, fmt.Errorf("bga: dispi id %#x", id)
	}
	write(io, regEnable, 0) // registers only take effect while disabled
	write(io, regXRes, uint16(w))
	write(io, regYRes, uint16(h))
	write(io, regBPP, BPP)
	write(io, regEnable, enEnabled|enLinearFrame)
	if got := read(io, regXRes); int(got) != w {
		return 0, fmt.Errorf("bga: asked for width %d, card has %d", w, got)
	}
	return int(read(io, regVirtWidth)) * BPP / 8, nil
}

var (
	mu     sync.Mutex
	screen *gfx.Framebuffer
)

// Screen is the framebuffer once the driver started (nil before, or with
// no BGA).
func Screen() *gfx.Framebuffer {
	mu.Lock()
	defer mu.Unlock()
	return screen
}

func init() { driver.Register(&display{}) }

type display struct{ dev pci.Device }

func (*display) Name() string { return "bga" }

func (d *display) Probe(env *driver.Env) bool {
	if env.Ports16 == nil || env.Ports32 == nil || env.MapMMIO == nil {
		return false
	}
	dev, ok := pci.Find(pci.Scan(env.Ports32), Vendor, Device)
	d.dev = dev
	return ok && read(env.Ports16, regID) >= idMin
}

func (d *display) Start(env *driver.Env) error {
	base := d.dev.MemBase(0)
	if base == 0 {
		return errors.New("BAR0 is not a memory BAR")
	}
	pitch, err := SetMode(env.Ports16, Width, Height)
	if err != nil {
		return err
	}
	mem, err := env.MapMMIO(base, uint64(pitch*Height))
	if err != nil {
		return fmt.Errorf("map %#x: %w", base, err)
	}
	fb, err := gfx.New(gfx.Words(mem), Width, Height, pitch)
	if err != nil {
		return err
	}
	mu.Lock()
	screen = fb
	mu.Unlock()
	env.Log.Infof("%dx%dx%d linear framebuffer at %#x (pitch %d)", Width, Height, BPP, base, pitch)
	return nil
}
