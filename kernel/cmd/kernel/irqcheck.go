//go:build burrow

package main

import (
	"fmt"
	"sync/atomic"
	"time"

	"burrow/kernel/arch/amd64"
	"burrow/kernel/drivers/pit"
	"burrow/kernel/selftest"
)

// irqCheck proves the interrupt path end to end (W7-S02): PIT channel 0
// one-shots raise IRQ 0, the nucleus wakes the thread blocked in Wait, and
// meanwhile other goroutines keep running (the waiting thread gave up its P).
//
// Only the first round may return early: the 8259 latched the BIOS's
// 18.2 Hz ticks while the line was masked. Reprogramming the PIT restarts
// its count, so later rounds each take one full period.
func irqCheck() selftest.Check {
	const rounds, period = 10, 10 * time.Millisecond
	return selftest.Check{Name: "irq", Run: func() (string, error) {
		line := amd64.NewIRQ(pit.IRQ)
		var spins atomic.Int64
		stop := make(chan struct{})
		go func() {
			for {
				select {
				case <-stop:
					return
				default:
					spins.Add(1)
					time.Sleep(time.Millisecond)
				}
			}
		}()
		defer close(stop)
		start := time.Now()
		for i := 0; i < rounds; i++ {
			pit.OneShot(amd64.Ports{}, period)
			if err := line.Wait(); err != nil {
				return "", fmt.Errorf("round %d: Wait: %w", i, err)
			}
			line.Ack()
		}
		took := time.Since(start)
		lo, hi := (rounds-1)*period, rounds*period+30*time.Millisecond
		if took < lo || took > hi {
			return "", fmt.Errorf("%d one-shots of %v took %v, want %v..%v", rounds, period, took, lo, hi)
		}
		if n := spins.Load(); n < 20 {
			return "", fmt.Errorf("another goroutine ran only %d times while waiting", n)
		}
		return fmt.Sprintf("%d pit one-shots on line 0 in %v, %d sleeps elsewhere meanwhile",
			rounds, took.Round(100*time.Microsecond), spins.Load()), nil
	}}
}
