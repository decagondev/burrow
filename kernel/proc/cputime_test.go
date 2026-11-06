package proc_test

import (
	"testing"
	"time"

	"burrow/kernel/proc"
)

// readWords reads n uint64s at va.
func readWords(p *proc.Process, va uint64, n int) []uint64 {
	b := read(p, va, 8*n)
	out := make([]uint64, n)
	for i := range out {
		out[i] = le.Uint64(b[8*i:])
	}
	return out
}

func TestCPUTimeSelfAndChildren(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.nu.SetTimes(p.TID(), 1500*time.Millisecond, 250*time.Millisecond)
	if up := r.call(p, 100, scratch); up < 0 { // times
		t.Fatalf("times = %d", up)
	}
	if w := readWords(p, scratch, 4); w[0] != 150 || w[1] != 25 || w[2] != 0 || w[3] != 0 {
		t.Fatalf("tms = %v", w)
	}
	if e := r.call(p, 98, 0, scratch); e != 0 { // getrusage(RUSAGE_SELF)
		t.Fatalf("getrusage = %d", e)
	}
	if w := readWords(p, scratch, 4); w[0] != 1 || w[1] != 500000 || w[2] != 0 || w[3] != 250000 {
		t.Fatalf("ru_utime/ru_stime = %v", w)
	}

	r.call(p, 57) // fork; the child computes 3 s, then exits
	kid := r.newestThread(p.TID())
	r.nu.SetTimes(kid, 3*time.Second, time.Second)
	r.nu.Inject(proc.Request{TID: kid, Nr: 231, Args: [6]uint64{0}})
	if pid := r.call(p, 61, ^uint64(0), 0, 0, scratch+512); pid <= 0 { // wait4(-1, 0, 0, &ru)
		t.Fatalf("wait4 = %d", pid)
	}
	if w := readWords(p, scratch+512, 4); w[0] != 3 || w[2] != 1 {
		t.Errorf("wait4's rusage = %v", w)
	}
	r.call(p, 100, scratch)
	if w := readWords(p, scratch, 4); w[2] != 300 || w[3] != 100 {
		t.Errorf("cutime/cstime = %v", w)
	}
	if e := r.call(p, 98, uint64(0xFFFFFFFFFFFFFFFF), scratch); e != 0 { // RUSAGE_CHILDREN
		t.Fatalf("getrusage(children) = %d", e)
	}
	if w := readWords(p, scratch, 5); w[0] != 3 || w[2] != 1 || w[4] == 0 {
		t.Errorf("children's rusage = %v", w)
	}
}
