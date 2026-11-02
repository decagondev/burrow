// Package selftest exercises the Go runtime as hosted by the nucleus:
// timers through the netpoller, goroutine scheduling, the garbage collector
// and the language features that lean on the runtime. Each check prints
// "[go-selftest] <name> ok (<detail>)" or returns an error naming what broke.
//
// It is plain Go with no hardware access, so `go test` runs the same checks
// (with smaller sizes) on the host.
package selftest

import (
	"fmt"
	"io"
	"time"
)

// Config sizes the checks. Kernel() is what the boot runs; tests use less.
type Config struct {
	SleepRounds int           // time.Sleep calls to time
	Sleep       time.Duration // each one
	SleepSlack  float64       // allowed overshoot, as a fraction of Sleep
}

// Kernel is the configuration the Go kernel runs at boot.
func Kernel() Config {
	return Config{SleepRounds: 10, Sleep: 100 * time.Millisecond, SleepSlack: 0.10}
}

type check struct {
	name string
	run  func(Config) (detail string, err error)
}

var checks = []check{
	{"sleep", checkSleep},
}

// Run executes every check in order and stops at the first failure.
func Run(w io.Writer, cfg Config) error {
	for _, c := range checks {
		detail, err := c.run(cfg)
		if err != nil {
			return fmt.Errorf("%s: %w", c.name, err)
		}
		fmt.Fprintf(w, "[go-selftest] %s ok (%s)\n", c.name, detail)
	}
	return nil
}

// checkSleep: timers go through the netpoller (epoll_pwait timeouts and the
// eventfd break); every sleep must last at least Sleep and at most
// Sleep*(1+SleepSlack).
func checkSleep(cfg Config) (string, error) {
	limit := cfg.Sleep + time.Duration(float64(cfg.Sleep)*cfg.SleepSlack)
	var worst time.Duration
	for i := 0; i < cfg.SleepRounds; i++ {
		start := time.Now()
		time.Sleep(cfg.Sleep)
		got := time.Since(start)
		if got < cfg.Sleep || got > limit {
			return "", fmt.Errorf("round %d slept %v, want %v..%v", i, got, cfg.Sleep, limit)
		}
		worst = max(worst, got)
	}
	return fmt.Sprintf("%d x %v, longest %v", cfg.SleepRounds, cfg.Sleep, worst.Round(10*time.Microsecond)), nil
}
