package proc

// waitany_test.go - waitAny reports what fired with reflect.Select's
// numbering, through the plain select (few channels) and the fallback
// (many), and blocks while nothing fires.

import (
	"testing"
	"time"
)

func TestWaitAnyReportsWhatFired(t *testing.T) {
	for _, n := range []int{0, 1, waitAnyFast, waitAnyFast + 3} {
		chans := make([]chan struct{}, n)
		waits := make([]<-chan struct{}, n)
		for i := range chans {
			chans[i] = make(chan struct{})
			waits[i] = chans[i]
		}
		if n > 0 {
			close(chans[n-1])
			if got := waitAny(nil, nil, waits); got != 2+n-1 {
				t.Errorf("%d channels, the last closed: %d, want %d", n, got, 2+n-1)
			}
			chans[n-1] = make(chan struct{})
			waits[n-1] = chans[n-1]
		}
		cancel := make(chan struct{})
		close(cancel)
		if got := waitAny(cancel, nil, waits); got != 0 {
			t.Errorf("%d channels, cancelled: %d, want 0", n, got)
		}
		if got := waitAny(nil, time.After(time.Millisecond), waits); got != 1 {
			t.Errorf("%d channels, deadline: %d, want 1", n, got)
		}
	}
}

func TestWaitAnyBlocksUntilSomethingFires(t *testing.T) {
	ch := make(chan struct{})
	done := make(chan int)
	go func() { done <- waitAny(nil, nil, []<-chan struct{}{ch}) }()
	select {
	case got := <-done:
		t.Fatalf("returned %d with nothing fired", got)
	case <-time.After(20 * time.Millisecond):
	}
	close(ch)
	if got := <-done; got != 2 {
		t.Errorf("got %d, want 2", got)
	}
}
