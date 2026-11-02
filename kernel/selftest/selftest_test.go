package selftest

import (
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
