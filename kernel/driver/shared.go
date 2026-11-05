// shared.go - interrupt lines that several devices share (W22-S01). QEMU's pc
// machine routes its four PCI interrupt links onto four 8259 lines, so PCI
// devices meet on one, and the nucleus lets one thread wait on a line
// (irq_wait answers EBUSY to a second). Lines gives each line one goroutine
// (Serve's) that calls every handler registered on it and acks once; each
// handler checks its own device and says whether it raised the interrupt.
// Never blocks a handler on another; never acks before every handler ran.
// Docs: docs/04-plan/waves/WAVE-22-sound-tablet.md; ADR-0005.
package driver

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"burrow/kernel/hal"
	"burrow/kernel/klog"
)

// Handler serves one device on a line: it reads its device's own status
// and returns true if that device raised the interrupt (after serving it).
type Handler func() (mine bool)

type sharedLine struct {
	mu        sync.Mutex
	names     []string
	handlers  []Handler
	unclaimed int  // interrupts no handler claimed
	warned    bool // the first unclaimed one was logged
}

// Lines is the kernel's table of interrupt lines and who serves each.
type Lines struct {
	mu    sync.Mutex
	irq   func(line int) hal.IRQLine
	log   *klog.Logger
	lines map[int]*sharedLine
}

// NewLines makes the table over irq (amd64.NewIRQ on Burrow, fakes in tests).
func NewLines(irq func(line int) hal.IRQLine, log *klog.Logger) *Lines {
	return &Lines{irq: irq, log: log, lines: map[int]*sharedLine{}}
}

// Serve adds h, named name, to line; the first handler on a line starts the
// line's goroutine. A driver registers before it lets its device interrupt.
func (ls *Lines) Serve(line int, name string, h Handler) {
	ls.mu.Lock()
	sl, ok := ls.lines[line]
	if !ok {
		sl = &sharedLine{}
		ls.lines[line] = sl
	}
	sl.mu.Lock()
	sl.names = append(sl.names, name)
	sl.handlers = append(sl.handlers, h)
	sl.mu.Unlock()
	ls.mu.Unlock()
	if !ok {
		Serve(ls.irq(line), func() { ls.dispatch(line, sl) })
	}
}

// dispatch runs every handler of the line once; an interrupt nobody claimed
// is counted, and the first one logged (a line that stays asserted with no
// owner would otherwise interrupt for ever in silence).
func (ls *Lines) dispatch(line int, sl *sharedLine) {
	sl.mu.Lock()
	hs := append([]Handler(nil), sl.handlers...)
	sl.mu.Unlock()
	claimed := false
	for _, h := range hs {
		if h() {
			claimed = true
		}
	}
	if claimed {
		return
	}
	sl.mu.Lock()
	sl.unclaimed++
	warn := !sl.warned
	sl.warned = true
	names := strings.Join(sl.names, ", ")
	sl.mu.Unlock()
	if warn && ls.log != nil {
		ls.log.Warnf("irq %d: an interrupt nobody claimed (served by %s)", line, names)
	}
}

// Unclaimed is how many interrupts on line no handler claimed.
func (ls *Lines) Unclaimed(line int) int {
	ls.mu.Lock()
	sl := ls.lines[line]
	ls.mu.Unlock()
	if sl == nil {
		return 0
	}
	sl.mu.Lock()
	defer sl.mu.Unlock()
	return sl.unclaimed
}

// String lists the lines in use and their drivers, one line each:
// "irq 11: e1000, ac97".
func (ls *Lines) String() string {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	nums := make([]int, 0, len(ls.lines))
	for n := range ls.lines {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	var b strings.Builder
	for _, n := range nums {
		sl := ls.lines[n]
		sl.mu.Lock()
		fmt.Fprintf(&b, "irq %d: %s\n", n, strings.Join(sl.names, ", "))
		sl.mu.Unlock()
	}
	return b.String()
}
