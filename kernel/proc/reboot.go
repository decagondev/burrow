package proc

// reboot.go - restarting the machine and turning it off (W28-S04):
// reboot(2) with Linux's magic numbers (root only; RESTART, POWER_OFF and
// HALT, which powers off: Burrow has no halted state; CAD_ON and CAD_OFF
// accepted and ignored), and the signals busybox's reboot, poweroff and
// halt send PID 1 (SIGTERM, SIGUSR2, SIGUSR1, from root), which PID 1, kept
// by the kernel, answers the same way. sync and syncfs have nothing to do:
// ext2 commits after every operation and /tmp's files write back at close
// (wfile.go). What restarting and powering off do is the kernel's
// (SetPower); this file only decides who may. Docs: POSIX_COVERAGE.md,
// docs/04-plan/waves/WAVE-28-more-devices.md.

import "syscall"

// reboot(2)'s commands and magic numbers (include/uapi/linux/reboot.h).
const (
	RebootRestart  = 0x01234567
	RebootHalt     = 0xCDEF0123
	RebootPowerOff = 0x4321FEDC
	rebootCADOn    = 0x89ABCDEF
	rebootCADOff   = 0
	rebootMagic1   = 0xFEE1DEAD
)

var rebootMagic2 = map[uint32]bool{672274793: true, 85072278: true, 369367448: true, 537993216: true}

// initSignals: what busybox's halt applets ask PID 1 for with a signal.
var initSignals = map[int]uint32{
	int(syscall.SIGTERM): RebootRestart,
	int(syscall.SIGUSR2): RebootPowerOff,
	int(syscall.SIGUSR1): RebootHalt,
}

// SetPower installs what a restart (RebootRestart) or a power-off
// (RebootPowerOff, RebootHalt) does; it need not return.
func (s *System) SetPower(f func(cmd uint32)) {
	s.mu.Lock()
	s.power = f
	s.mu.Unlock()
}

// doPower runs the installed action for cmd; 0, or -ENOSYS with none.
func (s *System) doPower(cmd uint32) int64 {
	s.mu.Lock()
	f := s.power
	s.mu.Unlock()
	if f == nil {
		return -int64(syscall.ENOSYS)
	}
	f(cmd)
	return 0
}

func sysRebootFn(p *Process, a [6]uint64) (int64, bool) {
	if uint32(a[0]) != rebootMagic1 || !rebootMagic2[uint32(a[1])] {
		return ok(-int64(syscall.EINVAL))
	}
	if c := p.Cred(); !c.Root() {
		return ok(-int64(syscall.EPERM))
	}
	switch cmd := uint32(a[2]); cmd {
	case rebootCADOn, rebootCADOff:
		return ok(0)
	case RebootRestart, RebootPowerOff, RebootHalt:
		return ok(p.sys.doPower(cmd))
	}
	return ok(-int64(syscall.EINVAL))
}

// killInit answers a signal to PID 1 that asks for a restart or a
// power-off; handled is false for any other signal.
func (s *System) killInit(from *Process, sig int) (res int64, handled bool) {
	cmd, ok := initSignals[sig]
	if !ok || from == nil {
		return 0, false
	}
	if c := from.Cred(); !c.Root() {
		return -int64(syscall.EPERM), true
	}
	return s.doPower(cmd), true
}

func sysSyncFn(*Process, [6]uint64) (int64, bool) { return ok(0) }

func init() {
	table[169] = sysRebootFn
	table[162] = sysSyncFn // sync
	table[306] = sysSyncFn // syncfs
}
