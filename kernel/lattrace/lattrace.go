// Package lattrace is /dev/lattrace (W25-S01): a window on the nucleus's
// round-trip stamps (nu_lattrace). Writing a system call's number traces
// it (and clears the sums), "off" stops; each open reads the sums once, as
// the average time a traced request spent in each step: from the gate to
// Go taking it, from there to Go's answer, from the answer to the thread
// running again, and from there to ring 3. Never changes what a call does,
// only what is measured. Docs: docs/04-plan/waves/WAVE-25-speed.md.
package lattrace

import (
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"burrow/kernel/fs/devfs"
)

// Steps names the four intervals of a round trip, in order.
var Steps = [4]string{"gate-taken", "taken-replied", "replied-resumed", "resumed-out"}

// Stats is what the nucleus summed for the traced call.
type Stats struct {
	On             bool
	Nr             uint64
	Count, Skipped uint64
	Steps          [4]time.Duration  // summed over Count requests
	GoCalls        map[uint64]uint64 // W25-S03: the Go kernel's own calls by number while tracing
}

// topCalls is how many of the Go kernel's most frequent calls Format lists.
const topCalls = 6

// Tracer is the nucleus's side (arch/amd64.LatTracer; a fake in tests).
type Tracer interface {
	Start(nr uint64) error
	Stop() error
	Read() (Stats, error)
}

// Device is /dev/lattrace.
type Device struct {
	t Tracer
	handle
}

// New is a /dev/lattrace over t.
func New(t Tracer) *Device { return &Device{t: t, handle: handle{t: t}} }

// Open gives each open its own reading of the sums.
func (d *Device) Open() (devfs.Device, error) { return &handle{t: d.t}, nil }

type handle struct {
	t    Tracer
	mu   sync.Mutex
	out  []byte
	read bool
}

func (h *handle) Read(b []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.read {
		s, err := h.t.Read()
		if err != nil {
			return 0, err
		}
		h.out, h.read = []byte(Format(s)), true
	}
	if len(h.out) == 0 {
		return 0, io.EOF
	}
	n := copy(b, h.out)
	h.out = h.out[n:]
	return n, nil
}

func (h *handle) Write(b []byte) (int, error) {
	word := strings.TrimSpace(string(b))
	if word == "off" {
		return len(b), h.t.Stop()
	}
	nr, err := strconv.ParseUint(word, 10, 64)
	if err != nil {
		return 0, fs.ErrInvalid
	}
	return len(b), h.t.Start(nr)
}

// Format is the text a read gives: the call, how many requests were
// summed, and each step's average in microseconds.
func Format(s Stats) string {
	on := 0
	if s.On {
		on = 1
	}
	var b strings.Builder
	fmt.Fprintf(&b, "nr %d on %d count %d skipped %d\n", s.Nr, on, s.Count, s.Skipped)
	for i, name := range Steps {
		avg := 0.0
		if s.Count > 0 {
			avg = float64(s.Steps[i]) / float64(s.Count) / float64(time.Microsecond)
		}
		fmt.Fprintf(&b, "%s %.2f us\n", name, avg)
	}
	formatGoCalls(&b, s)
	return b.String()
}

// formatGoCalls lists the Go kernel's own calls per traced request: the
// total, then the most frequent by number (W25-S03).
func formatGoCalls(b *strings.Builder, s Stats) {
	if s.Count == 0 || len(s.GoCalls) == 0 {
		return
	}
	nrs := make([]uint64, 0, len(s.GoCalls))
	var total uint64
	for nr, n := range s.GoCalls {
		nrs = append(nrs, nr)
		total += n
	}
	sort.Slice(nrs, func(i, j int) bool {
		if s.GoCalls[nrs[i]] != s.GoCalls[nrs[j]] {
			return s.GoCalls[nrs[i]] > s.GoCalls[nrs[j]]
		}
		return nrs[i] < nrs[j]
	})
	fmt.Fprintf(b, "go-calls %.2f per request\n", float64(total)/float64(s.Count))
	for i, nr := range nrs {
		if i == topCalls {
			break
		}
		fmt.Fprintf(b, "go-call %d %.2f per request\n", nr, float64(s.GoCalls[nr])/float64(s.Count))
	}
}
