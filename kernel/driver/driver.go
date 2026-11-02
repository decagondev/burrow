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

	"burrow/kernel/hal"
	"burrow/kernel/klog"
)

// Env is what a driver may touch.
type Env struct {
	Ports   hal.PortIO
	Ports32 hal.PortIO32
	IRQ     func(line int) hal.IRQLine
	Log     *klog.Logger // StartAll gives each driver its own "[name]" child
	Input   chan<- byte  // console input: keyboards and serial lines write here
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
