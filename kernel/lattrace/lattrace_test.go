package lattrace

// lattrace_test.go - /dev/lattrace over a fake tracer: a number starts
// tracing, "off" stops, anything else is refused, and each open reads the
// averages once.

import (
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"
	"time"
)

type fake struct {
	nr    uint64
	on    bool
	stats Stats
}

func (f *fake) Start(nr uint64) error { f.nr, f.on = nr, true; return nil }
func (f *fake) Stop() error           { f.on = false; return nil }
func (f *fake) Read() (Stats, error)  { s := f.stats; s.On, s.Nr = f.on, f.nr; return s, nil }

func TestDeviceStartsStopsAndReads(t *testing.T) {
	f := &fake{stats: Stats{Count: 4, Steps: [4]time.Duration{40 * time.Microsecond, 400 * time.Microsecond, 8 * time.Microsecond, 2 * time.Microsecond}}}
	d := New(f)
	if _, err := d.Write([]byte("39\n")); err != nil || !f.on || f.nr != 39 {
		t.Fatalf("write 39: %v, on %v nr %d", err, f.on, f.nr)
	}
	h, _ := d.Open()
	got, err := io.ReadAll(h)
	if err != nil {
		t.Fatal(err)
	}
	want := "nr 39 on 1 count 4 skipped 0\ngate-taken 10.00 us\ntaken-replied 100.00 us\nreplied-resumed 2.00 us\nresumed-out 0.50 us\n"
	if string(got) != want {
		t.Errorf("read:\n%s\nwant:\n%s", got, want)
	}
	if _, err := d.Write([]byte("off")); err != nil || f.on {
		t.Errorf("off: %v, on %v", err, f.on)
	}
	if _, err := d.Write([]byte("getpid")); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("a name was taken: %v", err)
	}
}

func TestFormatWithNothingCounted(t *testing.T) {
	if s := Format(Stats{}); !strings.Contains(s, "gate-taken 0.00 us") {
		t.Errorf("empty stats: %q", s)
	}
}
