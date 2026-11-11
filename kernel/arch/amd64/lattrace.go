//go:build burrow

package amd64

// lattrace.go - the nucleus's round-trip stamps (nu_lattrace, W25-S01)
// behind kernel/lattrace's Tracer. Never stamps anything itself: the
// nucleus takes the stamps; this only starts, stops and reads the sums.
// Docs: docs/02-architecture/NUCLEUS_ABI.md.

import (
	"time"
	"unsafe"

	"burrow/kernel/lattrace"
)

const nrLatTrace = 0x102B

// LatTracer is lattrace.Tracer over nu_lattrace.
type LatTracer struct{}

func (LatTracer) Start(nr uint64) error { _, err := raw(nrLatTrace, 0, uintptr(nr)); return err }
func (LatTracer) Stop() error           { _, err := raw(nrLatTrace, 1); return err }

func (LatTracer) Read() (lattrace.Stats, error) {
	var b [8]uint64
	if _, err := raw(nrLatTrace, 2, 0, uintptr(unsafe.Pointer(&b[0]))); err != nil {
		return lattrace.Stats{}, err
	}
	s := lattrace.Stats{On: b[0] != 0, Nr: b[1], Count: b[2], Skipped: b[3]}
	for i := range s.Steps {
		s.Steps[i] = time.Duration(b[4+i])
	}
	return s, nil
}
