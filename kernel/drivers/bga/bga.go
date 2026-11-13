// Package bga is the display (E07.F04). Since W21-S03 it first takes the
// mode the firmware set (stage2's VBE mode, in boot_info and checked by the
// nucleus, handed over as driver.Env.Framebuffer) on whatever adapter it
// is; only without one does it program the Bochs/QEMU adapter ("BGA", PCI
// 1234:1111, QEMU's -vga std) itself through its "dispi" registers. Either
// way it maps the framebuffer once and hands a gfx.Framebuffer to the
// kernel. It never assumes a size, a pitch or a pixel layout it was not
// given. Reference: the OSDev wiki "Bochs VBE Extensions";
// docs/04-plan/waves/WAVE-21-framebuffer.md.
package bga

import (
	"errors"
	"fmt"
	"sync"

	"burrow/kernel/bootinfo"
	"burrow/kernel/driver"
	"burrow/kernel/drivers/pci"
	"burrow/kernel/gfx"
	"burrow/kernel/hal"
)

const (
	Vendor = 0x1234
	Device = 0x1111

	// The mode DISPI sets when the firmware set none (W8; the fallback since W21).
	DispiWidth, DispiHeight, DispiBPP = 1024, 768, 32

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
	write(io, regBPP, DispiBPP)
	write(io, regEnable, enEnabled|enLinearFrame)
	if got := read(io, regXRes); int(got) != w {
		return 0, fmt.Errorf("bga: asked for width %d, card has %d", w, got)
	}
	return int(read(io, regVirtWidth)) * DispiBPP / 8, nil
}

// Mode is a framebuffer to map: where, how big, and its pixel layout.
type Mode struct {
	Phys                 uint64
	Width, Height, Pitch int
	BPP                  int
	Format               gfx.Format
	Source               string // "vbe", "gop" (the UEFI loader's, W26-S04) or "dispi"
}

// firmwareSources names the framebuffers a loader set before the kernel.
var firmwareSources = map[uint8]string{bootinfo.FbSourceVBE: "vbe", bootinfo.FbSourceGOP: "gop"}

// FirmwareMode turns the firmware's descriptor into a Mode; ok is false
// when there is none or its layout is one gfx cannot draw.
func FirmwareMode(fb bootinfo.Framebuffer) (Mode, bool, error) {
	source, ok := firmwareSources[fb.Source]
	if !ok {
		return Mode{}, false, nil
	}
	f, err := gfx.FormatFor(fb.BPP, gfx.Field(fb.Red), gfx.Field(fb.Green), gfx.Field(fb.Blue))
	if err != nil {
		return Mode{}, false, err
	}
	return Mode{Phys: fb.Phys, Width: fb.Width, Height: fb.Height, Pitch: fb.Pitch, BPP: fb.BPP,
		Format: f, Source: source}, true, nil
}

var (
	mu      sync.Mutex
	screen  *gfx.Framebuffer
	current Mode
)

// Phys is the framebuffer's physical address and pitch in bytes (0 before
// the driver started).
func Phys() (base uint64, pitchBytes int) {
	mu.Lock()
	defer mu.Unlock()
	return current.Phys, current.Pitch
}

// Current is the mode the display runs in (zero before the driver started).
func Current() Mode {
	mu.Lock()
	defer mu.Unlock()
	return current
}

// Screen is the framebuffer once the driver started (nil before, or with
// no display).
func Screen() *gfx.Framebuffer {
	mu.Lock()
	defer mu.Unlock()
	return screen
}

func init() { driver.Register(&display{}) }

type display struct {
	dev  pci.Device
	bga  bool // the Bochs adapter is there (DISPI works)
	firm bool // the firmware set a mode
}

func (*display) Name() string { return "bga" }

func (d *display) Probe(env *driver.Env) bool {
	if env.MapMMIO == nil {
		return false
	}
	_, d.firm = firmwareSources[env.Framebuffer.Source]
	if env.Ports16 != nil && env.Ports32 != nil {
		dev, ok := pci.Find(pci.Scan(env.Ports32), Vendor, Device)
		d.dev, d.bga = dev, ok && read(env.Ports16, regID) >= idMin
	}
	return d.firm || d.bga
}

// choose is the firmware's mode when it gave a usable one, else DISPI's.
func (d *display) choose(env *driver.Env) (Mode, error) {
	m, ok, err := FirmwareMode(env.Framebuffer)
	if err != nil {
		env.Log.Warnf("firmware framebuffer refused: %v; setting a mode through dispi", err)
	}
	if ok {
		if d.bga {
			if bar := d.dev.MemBase(0); bar != 0 && bar != m.Phys {
				env.Log.Warnf("firmware framebuffer at %#x, BAR0 at %#x: using the firmware's", m.Phys, bar)
			}
		}
		return m, nil
	}
	if !d.bga {
		return Mode{}, errors.New("no firmware framebuffer and no Bochs adapter")
	}
	base := d.dev.MemBase(0)
	if base == 0 {
		return Mode{}, errors.New("BAR0 is not a memory BAR")
	}
	pitch, err := SetMode(env.Ports16, DispiWidth, DispiHeight)
	if err != nil {
		return Mode{}, err
	}
	return Mode{Phys: base, Width: DispiWidth, Height: DispiHeight, Pitch: pitch, BPP: DispiBPP,
		Format: gfx.XRGB8888, Source: "dispi"}, nil
}

func (d *display) Start(env *driver.Env) error {
	m, err := d.choose(env)
	if err != nil {
		return err
	}
	mapFB := env.MapMMIO
	if env.MapMMIOWC != nil {
		mapFB = env.MapMMIOWC // every pixel a bus write otherwise (W21-S04)
	}
	mem, err := mapFB(m.Phys, uint64(m.Pitch*m.Height))
	if err != nil {
		return fmt.Errorf("map %#x: %w", m.Phys, err)
	}
	fb, err := gfx.New(mem, m.Width, m.Height, m.Pitch, m.Format)
	if err != nil {
		return err
	}
	mu.Lock()
	screen, current = fb, m
	mu.Unlock()
	env.Log.Infof("%dx%dx%d linear framebuffer at %#x (pitch %d, %s)", m.Width, m.Height, m.BPP, m.Phys, m.Pitch, m.Source)
	return nil
}
