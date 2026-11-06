//go:build burrow

package amd64

// cputime.go - CPU time from the nucleus (W23-S02): a thread's user and
// kernel time (nu_thread_times) and every online CPU's user, kernel and
// idle time and switches (nu_cpu_times). Never caches: each call reads the
// nucleus's counters. Docs: docs/02-architecture/NUCLEUS_ABI.md.

import (
	"time"
	"unsafe"

	"burrow/kernel/proc"
)

const (
	nrThreadTimes = 0x1029
	nrCPUTimes    = 0x102A
	maxCPUs       = 16 // SMP_MAX_CPUS
)

func (Nucleus) ThreadTimes(tid uint64) (user, sys time.Duration, err error) {
	var t [2]uint64
	if _, err := raw(nrThreadTimes, uintptr(tid), uintptr(unsafe.Pointer(&t[0]))); err != nil {
		return 0, 0, err
	}
	return time.Duration(t[0]), time.Duration(t[1]), nil
}

func (Nucleus) CPUTimes() []proc.CPUTime {
	var c [maxCPUs][4]uint64
	n, err := raw(nrCPUTimes, uintptr(unsafe.Pointer(&c[0])), maxCPUs)
	if err != nil {
		return nil
	}
	out := make([]proc.CPUTime, n)
	for i := range out {
		out[i] = proc.CPUTime{User: time.Duration(c[i][0]), Sys: time.Duration(c[i][1]),
			Idle: time.Duration(c[i][2]), Switches: c[i][3]}
	}
	return out
}
