package proc_test

// dump_test.go - the hang dump (W20-S01): a thread blocked in read on an
// empty pipe shows as in syscall 0, with its process.

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"burrow/kernel/proc"
)

func TestDumpShowsABlockedThread(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	r.call(p, 22, scratch) // pipe: fds 3 (read), 4 (write)
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 0, Args: [6]uint64{3, scratch + 64, 8}})
	time.Sleep(20 * time.Millisecond) // blocked in read
	var out bytes.Buffer
	r.sys.Dump(&out)
	got := out.String()
	for _, want := range []string{"[dump] pid 2 ", "/bin/prog", "tid 2 in syscall 0 for ", "goroutines in kernel/proc", "[dump] end"} {
		if !strings.Contains(got, want) {
			t.Errorf("the dump lacks %q:\n%s", want, got)
		}
	}
	r.call(p, 1, 4, scratch, 1) // let the read finish
}

// TestBUG0026AKilledProcessLeavesNoHandlers: a thread blocked in pause when
// its process is killed must not leave its handler goroutine waiting for
// ever (waitstress found dozens in a dump, one per killed sleeper).
func TestBUG0026AKilledProcessLeavesNoHandlers(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	_, other := r.spawnThread(p, scratch+0x800, 0, 0, 0)
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 34}) // the main thread pauses
	time.Sleep(20 * time.Millisecond)
	r.nu.Inject(proc.Request{TID: other, Nr: 231, Args: [6]uint64{3}}) // the other ends the process
	p.Wait()
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		var out bytes.Buffer
		r.sys.Dump(&out)
		if !strings.Contains(out.String(), "sysPauseFn") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("a pause handler outlived its process:\n%s", out.String())
		}
	}
}
