package proc_test

import (
	"slices"
	"sync"
	"syscall"
	"testing"

	"burrow/kernel/proc"
)

// TestReboot: reboot(2)'s magic numbers, root only, each command to the
// kernel's action; busybox's signals to PID 1 the same; sync succeeds
// (W28-S04).
func TestReboot(t *testing.T) {
	const (
		nrReboot, nrKill, nrSetuid, nrSync = 169, 62, 105, 162
		magic1, magic2                     = 0xFEE1DEAD, 672274793
	)
	r := newRig(t)
	var mu sync.Mutex
	var got []uint32
	r.sys.SetPower(func(cmd uint32) { mu.Lock(); got = append(got, cmd); mu.Unlock() })
	p := r.spawn()
	inval, perm := -int64(syscall.EINVAL), -int64(syscall.EPERM)
	if v := r.call(p, nrReboot, magic1, magic2, 0x12345); v != inval {
		t.Errorf("an unknown command: %d", v)
	}
	if v := r.call(p, nrReboot, 0x1234, magic2, proc.RebootRestart); v != inval {
		t.Errorf("a wrong magic number: %d", v)
	}
	for _, cmd := range []uint64{proc.RebootRestart, proc.RebootPowerOff, proc.RebootHalt, 0x89ABCDEF} {
		if v := r.call(p, nrReboot, magic1, 85072278, cmd); v != 0 {
			t.Errorf("command %#x: %d", cmd, v)
		}
	}
	if v := r.call(p, nrKill, 1, uint64(syscall.SIGTERM)); v != 0 {
		t.Errorf("SIGTERM to init: %d", v)
	}
	if v := r.call(p, nrKill, 1, uint64(syscall.SIGUSR2)); v != 0 {
		t.Errorf("SIGUSR2 to init: %d", v)
	}
	if v := r.call(p, nrSync); v != 0 {
		t.Errorf("sync: %d", v)
	}
	want := []uint32{proc.RebootRestart, proc.RebootPowerOff, proc.RebootHalt, proc.RebootRestart, proc.RebootPowerOff}
	if r.call(p, nrSetuid, 1000) != 0 {
		t.Fatal("setuid")
	}
	if v := r.call(p, nrReboot, magic1, magic2, proc.RebootRestart); v != perm {
		t.Errorf("a user's reboot: %d", v)
	}
	if v := r.call(p, nrKill, 1, uint64(syscall.SIGTERM)); v != perm {
		t.Errorf("a user's SIGTERM to init: %d", v)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(got, want) {
		t.Fatalf("actions %#x, want %#x (a user's must not be carried out)", got, want)
	}
}
