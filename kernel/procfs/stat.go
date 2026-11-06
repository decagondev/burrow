package procfs

// stat.go - /proc/stat (W23-S02): the CPUs' time in ticks, all of them on
// the "cpu" line and each on its own (user, nice, system, idle, then six
// zeros: no iowait, interrupts, steal or guests are counted apart), the
// context switches, the boot time, the processes created and running.
// Docs: WAVE-23-proc-virtio.md.

import (
	"fmt"
	"strings"
	"time"
)

// CPUTime is one CPU's time since boot.
type CPUTime struct {
	User, Sys, Idle time.Duration
	Switches        uint64
}

func cpuLine(name string, c CPUTime) string {
	return fmt.Sprintf("%s %d 0 %d %d 0 0 0 0 0 0\n", name, ticks(c.User), ticks(c.Sys), ticks(c.Idle))
}

func statFile(f *FS) string {
	cpus := f.src.CPUs()
	var all CPUTime
	for _, c := range cpus {
		all.User, all.Sys, all.Idle = all.User+c.User, all.Sys+c.Sys, all.Idle+c.Idle
		all.Switches += c.Switches
	}
	var b strings.Builder
	b.WriteString(cpuLine("cpu ", all))
	for i, c := range cpus {
		b.WriteString(cpuLine(fmt.Sprintf("cpu%d", i), c))
	}
	l := f.src.Load()
	fmt.Fprintf(&b, "intr 0\nctxt %d\nbtime %d\nprocesses %d\nprocs_running %d\nprocs_blocked 0\nsoftirq 0\n",
		all.Switches, f.st.Boot.Unix(), l.LastPID, max(1, l.Running))
	return b.String()
}

// idle is every CPU's idle time together (/proc/uptime's second number).
func idle(f *FS) time.Duration {
	var sum time.Duration
	for _, c := range f.src.CPUs() {
		sum += c.Idle
	}
	return sum
}
