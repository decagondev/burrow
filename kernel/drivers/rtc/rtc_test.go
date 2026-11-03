package rtc

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"burrow/kernel/driver"
	"burrow/kernel/haltest"
	"burrow/kernel/klog"
)

// cmos holds 2026-11-03 21:07:45 in BCD and 24-hour form, as QEMU sets it.
func cmos() *haltest.FakeCMOS {
	c := &haltest.FakeCMOS{}
	r := &c.Regs
	r[regSeconds], r[regMinutes], r[regHours] = 0x45, 0x07, 0x21
	r[regDay], r[regMonth], r[regYear], r[regCentury] = 0x03, 0x11, 0x26, 0x20
	r[regStatusB] = status24h
	return c
}

var want = time.Date(2026, 11, 3, 21, 7, 45, 0, time.UTC)

func TestReadBCD(t *testing.T) {
	c := cmos()
	c.Updating = 5 // the first reads land in an update
	if got, err := Read(c); err != nil || !got.Equal(want) {
		t.Errorf("Read = %v, %v; want %v", got, err, want)
	}
}

func TestReadBinary12Hour(t *testing.T) {
	c := cmos()
	r := &c.Regs
	r[regStatusB] = statusBinary // binary, 12-hour
	r[regSeconds], r[regMinutes], r[regHours] = 45, 7, 9|hourPM
	r[regDay], r[regMonth], r[regYear], r[regCentury] = 3, 11, 26, 20
	if got, err := Read(c); err != nil || !got.Equal(want) {
		t.Errorf("9 PM in binary = %v, %v", got, err)
	}
	r[regHours] = 12 // 12 AM is midnight
	if got, _ := Read(c); got.Hour() != 0 {
		t.Errorf("12 AM = hour %d", got.Hour())
	}
	r[regHours] = 12 | hourPM // 12 PM is noon
	if got, _ := Read(c); got.Hour() != 12 {
		t.Errorf("12 PM = hour %d", got.Hour())
	}
}

// A tick between two snapshots: the first read is torn (the minute changed
// under it), so Read takes the next pair that agrees.
func TestReadTorn(t *testing.T) {
	c := cmos()
	reads := 0
	c.Tick = func(r *[128]byte) {
		if reads++; reads == 3 { // during the first snapshot
			r[regSeconds], r[regMinutes] = 0x00, 0x08
		}
	}
	got, err := Read(c)
	if want := time.Date(2026, 11, 3, 21, 8, 0, 0, time.UTC); err != nil || !got.Equal(want) {
		t.Errorf("torn read = %v, %v; want %v", got, err, want)
	}
}

func TestReadFailures(t *testing.T) {
	c := cmos()
	c.Updating = 1 << 30
	if _, err := Read(c); err != errBusy {
		t.Errorf("a stuck update = %v", err)
	}
	c = cmos()
	c.Regs[regMonth] = 0x13
	if _, err := Read(c); err == nil {
		t.Error("month 13 accepted")
	}
	c = cmos()
	c.Regs[regCentury] = 0 // no century register
	if got, _ := Read(c); got.Year() != 2026 {
		t.Errorf("without a century: %d", got.Year())
	}
}

func TestDriverSetsClock(t *testing.T) {
	var log bytes.Buffer
	var set time.Time
	env := &driver.Env{Ports: cmos(), Log: klog.New(&log, klog.Info), SetClock: func(t time.Time) { set = t }}
	d := &clock{}
	if !d.Probe(env) {
		t.Fatal("no CMOS found")
	}
	if err := d.Start(env); err != nil || !set.Equal(want) {
		t.Fatalf("Start = %v, set %v", err, set)
	}
	if !strings.Contains(log.String(), "wall clock 2026-11-03 21:07:45 UTC") {
		t.Errorf("log %q", log.String())
	}
	none := haltest.NewFakePorts()
	none.Values[portData] = 0xFF // a floating bus
	if (&clock{}).Probe(&driver.Env{Ports: none}) {
		t.Error("found a CMOS on a machine without one")
	}
}
