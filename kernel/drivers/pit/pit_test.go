package pit

import (
	"bytes"
	"testing"
	"time"

	"burrow/kernel/haltest"
)

func TestCounts(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want uint16
	}{
		{10 * time.Millisecond, 11931},
		{0, 1},
		{time.Second, 0xFFFF},
	} {
		if got := Counts(c.d); got != c.want {
			t.Errorf("Counts(%v) = %d, want %d", c.d, got, c.want)
		}
	}
}

func TestOneShotWritesModeThenLowHigh(t *testing.T) {
	io := haltest.NewFakePorts()
	OneShot(io, 10*time.Millisecond)
	if got := io.BytesWrittenTo(portCmd); !bytes.Equal(got, []byte{0x30}) {
		t.Errorf("command port got % x, want 30", got)
	}
	if got := io.BytesWrittenTo(portCh0); !bytes.Equal(got, []byte{0x9b, 0x2e}) { // 11931
		t.Errorf("channel 0 got % x, want 9b 2e", got)
	}
}
