// notes.go keeps one line per USB device a host controller driver found,
// bound or not, enumerated or failed (W27-S05), for the hardware report:
// what a tester's machine has on its ports is what the report must show.
package usb

import "sync"

var (
	notesMu sync.Mutex
	notes   []string
)

// Note records a device's line ("port 5: high-speed 0627:0001 ...").
func Note(line string) {
	notesMu.Lock()
	defer notesMu.Unlock()
	notes = append(notes, line)
}

// Notes is every line recorded, in order.
func Notes() []string {
	notesMu.Lock()
	defer notesMu.Unlock()
	return append([]string(nil), notes...)
}
