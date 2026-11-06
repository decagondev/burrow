package proc

import (
	"testing"
	"time"
)

// TestBUG0029StateIsNotATimer: a thread is sleeping ('S') only when it has
// been in a call that waits for a while. Opening /proc/self/stat on a
// loaded machine can take longer than blockedAfter, and the reader must
// still be running, as on Linux (proctest's stat-head failed under make test).
func TestBUG0029StateIsNotATimer(t *testing.T) {
	now := time.Now().UnixNano()
	long := now - int64(10*blockedAfter)
	for _, c := range []struct {
		nr      int64
		since   int64
		running bool
	}{
		{-1, long, true},   // in ring 3
		{257, long, true},  // openat, however long it took
		{0, now, true},     // read, just begun
		{0, long, false},   // read, waiting
		{61, long, false},  // wait4, waiting
		{202, long, false}, // futex, waiting
		{1, long, true},    // write: not a call that waits here
	} {
		th := &Thread{}
		th.inNr.Store(c.nr)
		th.inSince.Store(c.since)
		if got := th.running(now); got != c.running {
			t.Errorf("syscall %d since %v ago: running %v, want %v", c.nr, time.Duration(now-c.since), got, c.running)
		}
	}
}
