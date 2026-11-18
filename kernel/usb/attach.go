// attach.go is where an enumerated device meets the class drivers
// (W28-S02): a host controller driver, or a hub (hub.go) through its host
// controller, enumerates a device on a port and calls Connect, which binds
// its interfaces and logs one line per device, the same for a root-hub
// port ("port 5: ...") and a hub's ("port 1.3: ...": the root port, then
// each hub's port), and records it for the hardware report (Note). HubHost
// is what a host controller offers a hub driver. Never touches a
// controller. Docs: docs/04-plan/waves/WAVE-28-more-devices.md (S02),
// ADR-0014.
package usb

import (
	"fmt"

	"burrow/kernel/driver"
)

// Attached is an enumerated device: what Bind and the report line need.
type Attached struct {
	Dev     Device
	Path    string // "5", "5.2": the root-hub port, then each hub's port
	Speed   Speed
	Desc    DeviceDescriptor
	Config  Config
	Product string
}

// HubHost is implemented by a host controller's Device when that device
// may be a hub: the hub driver tells the controller what the hub is and
// asks it to address the devices on the hub's ports.
type HubHost interface {
	// MakeHub records that the device is a hub with ports downstream
	// ports, its transaction translator's think time (0-3, in units of 8
	// full-speed bit times) and whether it has one TT per port.
	MakeHub(ports, thinkTime int, multiTT bool) error
	// Attach enumerates the device on downstream port, which the hub has
	// reset and enabled at speed.
	Attach(port int, speed Speed) (Attached, error)
	// Detach forgets the device on port (and any below it): its slot is
	// freed and its pipes fail from then on.
	Detach(port int)
	// Path is the device's own path ("5", "5.2").
	Path() string
	// Depth is how many hubs are above the device (0 on a root port).
	Depth() int
	// LinkSpeed is the device's own speed.
	LinkSpeed() Speed
}

// Connect binds a's interfaces and logs and notes its line.
func Connect(env *driver.Env, a Attached) {
	bound, err := Bind(env, a.Dev, a.Config)
	what := ""
	for _, b := range bound {
		what += fmt.Sprintf("; interface %d: %s", b.Interface, b.What)
	}
	if err != nil {
		what = "; " + err.Error()
	}
	line := fmt.Sprintf("port %s: %v %04x:%04x %q%s", a.Path, a.Speed, a.Desc.Vendor, a.Desc.Product, a.Product, what)
	env.Log.Infof("%s", line)
	Note(line) // the hardware report (W27-S05)
}

// NotConnected logs and notes a port whose device did not enumerate.
func NotConnected(env *driver.Env, path string, err error) {
	env.Log.Warnf("port %s: %v", path, err)
	Note(fmt.Sprintf("port %s: not enumerated: %v", path, err))
}

// Disconnected logs and notes a device that went away.
func Disconnected(env *driver.Env, path string) {
	line := fmt.Sprintf("port %s: disconnected", path)
	env.Log.Infof("%s", line)
	Note(line)
}
