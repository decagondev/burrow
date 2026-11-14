// route.go - where an interrupt line goes, as the nucleus reports it
// (nu_irq_route, W27-S01): through the 8259, an I/O APIC pin with its
// trigger and polarity, or an MSI vector. For `irqs` and the hardware
// report; nothing decides anything from it. Never guesses a field the
// nucleus did not set. Docs: nucleus/irq.h; ADR-0014.
package driver

import "fmt"

// RouteKind is how a line reaches the CPU.
type RouteKind uint8

const (
	RouteNone   RouteKind = 0
	RoutePIC    RouteKind = 1 // the 8259 in virtual-wire mode
	RouteIOAPIC RouteKind = 2
	RouteMSI    RouteKind = 3
)

// The encoding, mirrored from nucleus/irq.h.
const (
	routeLevel     = 1 << 8
	routeActiveLow = 1 << 9
	routeShift     = 16
)

// Route is one line's path.
type Route struct {
	Kind      RouteKind
	Where     uint32 // the 8259 line, the I/O APIC's GSI, or the MSI vector
	Level     bool
	ActiveLow bool
}

// ParseRoute decodes nu_irq_route's answer.
func ParseRoute(v uint64) Route {
	return Route{
		Kind:      RouteKind(v & 0xFF),
		Where:     uint32(v >> routeShift),
		Level:     v&routeLevel != 0,
		ActiveLow: v&routeActiveLow != 0,
	}
}

func (r Route) String() string {
	switch r.Kind {
	case RoutePIC:
		return fmt.Sprintf("8259 line %d", r.Where)
	case RouteIOAPIC:
		trig, pol := "edge", "high"
		if r.Level {
			trig = "level"
		}
		if r.ActiveLow {
			pol = "low"
		}
		return fmt.Sprintf("i/o apic gsi %d %s %s", r.Where, trig, pol)
	case RouteMSI:
		return fmt.Sprintf("msi vector %#x", r.Where)
	}
	return "not routed"
}
