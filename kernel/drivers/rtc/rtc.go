// Package rtc reads the PC's CMOS real-time clock (W14-S04) once at boot and
// sets the wall clock from it (driver.Env.SetClock): from then on the Go
// runtime's time.Now, CLOCK_REALTIME for user programs and file dates are
// real. It never writes the CMOS and never reads it again; the TSC keeps
// time after boot. The clock is taken as UTC (QEMU's -rtc base=utc, the
// default). Reference: the OSDev wiki "CMOS", Linux's mc146818 code.
package rtc

import (
	"errors"
	"fmt"
	"time"

	"burrow/kernel/driver"
	"burrow/kernel/hal"
)

const (
	portIndex uint16 = 0x70
	portData  uint16 = 0x71

	regSeconds = 0x00
	regMinutes = 0x02
	regHours   = 0x04
	regDay     = 0x07
	regMonth   = 0x08
	regYear    = 0x09
	regStatusA = 0x0A
	regStatusB = 0x0B
	regCentury = 0x32 // the ACPI FADT may name another; QEMU and most PCs use this one

	statusUIP    = 0x80 // A: an update is in progress, the registers may be torn
	statusBinary = 0x04 // B: binary, not BCD
	status24h    = 0x02 // B: 24-hour, not 12-hour
	hourPM       = 0x80 // in the hours register in 12-hour form

	tries = 1000 // reads of status A before giving up on the update ending
)

var errBusy = errors.New("cmos: update in progress never ended")

// fields are the date registers in one read: seconds, minutes, hours, day,
// month, year, century.
type fields [7]byte

var fieldRegs = fields{regSeconds, regMinutes, regHours, regDay, regMonth, regYear, regCentury}

func reg(io hal.PortIO, r byte) byte {
	io.Out8(portIndex, r)
	return io.In8(portData)
}

// snapshot waits out an update in progress and reads every date register.
func snapshot(io hal.PortIO) (fields, error) {
	var f fields
	for i := 0; reg(io, regStatusA)&statusUIP != 0; i++ {
		if i == tries {
			return f, errBusy
		}
	}
	for i, r := range fieldRegs {
		f[i] = reg(io, r)
	}
	return f, nil
}

// Read returns the CMOS clock's time. An update may begin between the
// status check and the reads, so it reads until two snapshots agree.
func Read(io hal.PortIO) (time.Time, error) {
	prev, err := snapshot(io)
	if err != nil {
		return time.Time{}, err
	}
	for i := 0; ; i++ {
		cur, err := snapshot(io)
		if err != nil {
			return time.Time{}, err
		}
		if cur == prev {
			return decode(cur, reg(io, regStatusB))
		}
		if i == tries {
			return time.Time{}, errBusy
		}
		prev = cur
	}
}

func bcd(b byte) int { return int(b>>4)*10 + int(b&0x0F) }

// decode turns the registers into a time, from BCD or binary and from 12-
// or 24-hour form as status register B says.
func decode(f fields, statusB byte) (time.Time, error) {
	pm := f[2]&hourPM != 0
	f[2] &^= hourPM
	v := make([]int, len(f))
	for i, b := range f {
		v[i] = int(b)
		if statusB&statusBinary == 0 {
			v[i] = bcd(b)
		}
	}
	sec, min, hour, day, month, year, century := v[0], v[1], v[2], v[3], v[4], v[5], v[6]
	if statusB&status24h == 0 { // 12-hour: 12 AM is 0, 12 PM is 12
		hour %= 12
		if pm {
			hour += 12
		}
	}
	if century < 19 || century > 99 {
		century = 20 // no century register: assume this one
	}
	if year > 99 || sec > 59 || min > 59 || hour > 23 || day < 1 || day > 31 || month < 1 || month > 12 {
		return time.Time{}, fmt.Errorf("cmos: nonsense date %d-%02d-%02d %02d:%02d:%02d", year, month, day, hour, min, sec)
	}
	return time.Date(century*100+year, time.Month(month), day, hour, min, sec, 0, time.UTC), nil
}

func init() { driver.Register(&clock{}) }

type clock struct{}

func (*clock) Name() string { return "rtc" }

// Probe: without a CMOS the data port floats to 0xFF.
func (*clock) Probe(env *driver.Env) bool { return reg(env.Ports, regStatusB) != 0xFF }

func (*clock) Start(env *driver.Env) error {
	t, err := Read(env.Ports)
	if err != nil {
		return err
	}
	if env.SetClock != nil {
		env.SetClock(t)
	}
	env.Log.Infof("wall clock %s", t.Format("2006-01-02 15:04:05 UTC"))
	return nil
}
