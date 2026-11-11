package proc

// waitany.go - poll's, select's and epoll's wait (W25-S04): block until a
// signal cancels the call, the deadline passes or one of the files'
// channels changes, and say which, as reflect.Select's index would (0 the
// cancel, 1 the deadline, 2+i waits[i]). Most waits watch a few files (a
// socket and a pipe, X's clients), so up to waitAnyFast channels go
// through a plain select statement (nil channels never fire), which costs
// a fraction of building reflect.SelectCases each time; more fall back to
// reflect.Select. Never consumes anything but the channel it reports.
// Docs: docs/04-plan/waves/WAVE-25-speed.md.

import (
	"reflect"
	"time"
)

const waitAnyFast = 4 // channels served by the plain select

// waitAny blocks as described above and returns the index of what fired.
func waitAny(cancel <-chan struct{}, deadline <-chan time.Time, waits []<-chan struct{}) int {
	if len(waits) > waitAnyFast {
		cases := []reflect.SelectCase{
			{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(cancel)},
			{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(deadline)},
		}
		for _, ch := range waits {
			cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)})
		}
		chosen, _, _ := reflect.Select(cases)
		return chosen
	}
	var w [waitAnyFast]<-chan struct{}
	copy(w[:], waits)
	select {
	case <-cancel:
		return 0
	case <-deadline:
		return 1
	case <-w[0]:
		return 2
	case <-w[1]:
		return 3
	case <-w[2]:
		return 4
	case <-w[3]:
		return 5
	}
}
