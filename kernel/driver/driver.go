// Package driver is the registry every device driver plugs into (E06.F02).
// A driver package registers itself from init(); the kernel imports it for
// that side effect and calls StartAll once. Adding a driver never means
// editing a list somewhere else (Open/Closed).
//
// Drivers see hardware only through Env (Dependency Inversion), so the same
// driver runs on the target with arch/amd64 and in host tests with haltest.
package driver

import (
	"fmt"
	"runtime"
	"sort"
	"sync"
	"time"

	"burrow/kernel/bootinfo"
	"burrow/kernel/fs/devfs"
	"burrow/kernel/hal"
	"burrow/kernel/klog"
)

// Env is what a driver may touch.
type Env struct {
	Ports   hal.PortIO
	Ports16 hal.PortIO16
	Ports32 hal.PortIO32
	IRQ     func(line int) hal.IRQLine
	// Lines, if set, serves interrupt lines several devices share (PCI,
	// W22-S01); a PCI driver registers its handler there rather than
	// waiting on the line itself.
	Lines *Lines
	Log   *klog.Logger // StartAll gives each driver its own "[name]" child
	Input chan<- byte  // console input: keyboards and serial lines write here
	// Scancodes, if set, sees every raw keyboard byte first (/dev/input/event0,
	// W11); grabbed means a program holds the keyboard, so nothing goes to Input.
	Scancodes func(sc byte) (grabbed bool)
	// Mouse, if set, gets every mouse packet in screen directions (dy > 0
	// down, wheel > 0 up; buttons: bit 0 left, 1 right, 2 middle; W14-S02).
	Mouse func(dx, dy, wheel int, buttons uint8)
	// Tablet, if set, makes the event device of an absolute pointer (its
	// name and axes' ranges; virtioinput, W22-S04) and returns what gets each
	// of its events (an evdev type, code and value: EV_ABS, EV_KEY, EV_SYN).
	Tablet func(TabletInfo) func(typ, code uint16, value int32)
	Dev    *devfs.FS // where a driver registers its device files (/dev)
	// MapMMIO maps device memory (a PCI BAR) and returns it.
	MapMMIO func(pa, n uint64) ([]byte, error)
	// MapMMIOWC, if set, maps it write-combining (a framebuffer, W21-S04).
	MapMMIOWC func(pa, n uint64) ([]byte, error)
	// DMAPage hands out one zeroed 4 KiB page a device may read and write:
	// its physical address and its bytes. Never Go heap: those pages move
	// and are filled on first touch (W17-S01, PIT-44).
	DMAPage func() (pa uint64, mem []byte, err error)
	// SetClock, if set, sets the wall clock (the RTC driver calls it once).
	SetClock func(time.Time)
	// Framebuffer is the display mode the firmware set (stage2's VBE mode,
	// checked by the nucleus; W21-S03); Source 0 when there is none.
	Framebuffer bootinfo.Framebuffer
}

// TabletInfo is what an absolute pointer says about itself.
type TabletInfo struct {
	Name                   string
	XMin, XMax, YMin, YMax int32
}

// Driver is one device driver.
type Driver interface {
	Name() string
	Probe(env *Env) bool  // is the device there? Must not leave it half set up.
	Start(env *Env) error // set it up and start its goroutines
}

// Registry holds drivers by name. Most code uses the package-level one.
type Registry struct {
	mu      sync.Mutex
	drivers map[string]Driver
}

func NewRegistry() *Registry { return &Registry{drivers: map[string]Driver{}} }

var std = NewRegistry()

// Register adds d to the kernel's registry; call it from init().
func Register(d Driver) { std.Register(d) }

// StartAll starts every registered driver; see Registry.StartAll.
func StartAll(env *Env) []string { return std.StartAll(env) }

// Register panics on a duplicate name: two drivers claiming one name is a
// build mistake, not a runtime condition.
func (r *Registry) Register(d Driver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.drivers[d.Name()]; dup {
		panic(fmt.Sprintf("driver: %q registered twice", d.Name()))
	}
	r.drivers[d.Name()] = d
}

// StartAll probes and starts every driver in name order (deterministic boot
// logs) and returns the names that started. A missing device or a failed
// start is logged and skipped: one broken driver must not stop the boot.
func (r *Registry) StartAll(env *Env) []string {
	r.mu.Lock()
	names := make([]string, 0, len(r.drivers))
	for n := range r.drivers {
		names = append(names, n)
	}
	r.mu.Unlock()
	sort.Strings(names)

	log := env.Log.With("driver")
	var started []string
	for _, n := range names {
		d := r.drivers[n]
		denv := *env
		denv.Log = env.Log.With(n)
		switch {
		case !d.Probe(&denv):
			log.Infof("%s: no device", n)
		default:
			if err := d.Start(&denv); err != nil {
				log.Errorf("%s: %v", n, err)
				continue
			}
			started = append(started, n)
		}
	}
	log.Infof("started: %v", started)
	return started
}

// Serve runs handle once per interrupt on line, on a goroutine locked to its
// own OS thread: that thread is the one parked in the nucleus while the line
// is quiet (ADR-0005). handle drains the device; Serve acks afterwards. The
// returned channel closes when Wait fails (a test closed a fake line).
func Serve(line hal.IRQLine, handle func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		for line.Wait() == nil {
			handle()
			line.Ack()
		}
	}()
	return done
}
