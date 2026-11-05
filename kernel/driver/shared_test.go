package driver

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"burrow/kernel/hal"
	"burrow/kernel/haltest"
	"burrow/kernel/klog"
)

// dev is a fake device on a shared line: raise sets its status bit and
// fires the line; its handler claims the interrupt only when the bit is set.
type dev struct {
	line    *haltest.FakeIRQ
	pending atomic.Bool
	served  atomic.Int64
}

func (d *dev) raise() { d.pending.Store(true); d.line.Fire() }

func (d *dev) handle() bool {
	if !d.pending.Swap(false) {
		return false
	}
	d.served.Add(1)
	return true
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); !ok(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// W22-S01: two devices on one line are each served for the interrupts they
// raised, and only those, whichever raises first; one goroutine waits on
// the line (a second irq_wait would get EBUSY from the nucleus).
func TestSharedLineServesEachDevice(t *testing.T) {
	line := haltest.NewFakeIRQ()
	defer line.Close()
	var opened int
	var mu sync.Mutex
	var log strings.Builder
	ls := NewLines(func(n int) hal.IRQLine {
		mu.Lock()
		opened++
		mu.Unlock()
		if n != 11 {
			t.Errorf("line %d opened", n)
		}
		return line
	}, klog.New(&log, klog.Info))
	a, b := &dev{line: line}, &dev{line: line}
	ls.Serve(11, "e1000", a.handle)
	ls.Serve(11, "ac97", b.handle)
	for i := 0; i < 50; i++ {
		if i%2 == 0 {
			a.raise()
		} else {
			b.raise()
		}
		waitUntil(t, "the interrupt served", func() bool { return a.served.Load()+b.served.Load() == int64(i+1) })
	}
	if a.served.Load() != 25 || b.served.Load() != 25 {
		t.Errorf("served %d and %d, want 25 each", a.served.Load(), b.served.Load())
	}
	if opened != 1 {
		t.Errorf("line opened %d times, want once", opened)
	}
	if got := ls.String(); got != "irq 11: e1000, ac97\n" {
		t.Errorf("String = %q", got)
	}
	waitUntil(t, "50 acks", func() bool { return line.Acks() == 50 })
	if ls.Unclaimed(11) != 0 {
		t.Errorf("unclaimed %d", ls.Unclaimed(11))
	}
}

// An interrupt no device claims is counted and logged once, not dropped in
// silence (a level-triggered line with no owner storms).
func TestSharedLineCountsUnclaimed(t *testing.T) {
	line := haltest.NewFakeIRQ()
	defer line.Close()
	var log strings.Builder
	var logMu sync.Mutex
	ls := NewLines(func(int) hal.IRQLine { return line }, klog.New(lockedWriter{&log, &logMu}, klog.Info))
	a := &dev{line: line}
	ls.Serve(5, "e1000", a.handle)
	for i := 1; i <= 3; i++ {
		line.Fire() // nobody's status bit is set
		waitUntil(t, "the unclaimed count", func() bool { return ls.Unclaimed(5) == i })
	}
	logMu.Lock()
	defer logMu.Unlock()
	if n := strings.Count(log.String(), "nobody claimed"); n != 1 {
		t.Errorf("logged %d times, want once:\n%s", n, log.String())
	}
}

type lockedWriter struct {
	b  *strings.Builder
	mu *sync.Mutex
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}
