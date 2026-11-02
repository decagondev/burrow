package selftest

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// The same checks as at boot, sized for a quick host run. The host is not
// a quiet machine, so the sleep bound is loose here; the boot config is tight.
func TestRunOnHost(t *testing.T) {
	var out strings.Builder
	cfg := Config{
		SleepRounds: 3, Sleep: 10 * time.Millisecond, SleepSlack: 5,
		PingPongs: 1000,
		GCTotal:   256 * mib, GCWindow: 8 * mib, HeapLimit: 1024 * mib,
	}
	if err := Run(&out, cfg); err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if !strings.Contains(out.String(), "[go-selftest] "+c.name+" ok") {
			t.Errorf("missing marker for %s in %q", c.name, out.String())
		}
	}
}

func TestRunChecksMarkersAndErrors(t *testing.T) {
	var out strings.Builder
	ok := Check{"fine", func() (string, error) { return "detail", nil }}
	if err := RunChecks(&out, ok); err != nil || out.String() != "[go-selftest] fine ok (detail)\n" {
		t.Fatalf("RunChecks = %v, output %q", err, out.String())
	}
	bad := Check{"broken", func() (string, error) { return "", errors.New("boom") }}
	if err := RunChecks(&out, bad, ok); err == nil || err.Error() != "broken: boom" {
		t.Fatalf("RunChecks(bad) = %v, want \"broken: boom\"", err)
	}
}
