package proc_test

import (
	"syscall"
	"testing"

	"burrow/kernel/proc"
)

const (
	pAll, pPID                   = 0, 1
	wNoHang, wExited, wNoWait    = 1, 4, 0x01000000
	siSigno, siCode, siPid, siSt = 0, 8, 16, 24
)

// waitid as QProcess uses it: an exited child's siginfo, WNOHANG while it
// runs, WNOWAIT leaving it for a later wait.
func TestWaitid(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	if e := r.call(p, 247, pAll, 0, scratch, wExited); e != -int64(syscall.ECHILD) {
		t.Errorf("waitid with no children = %d", e)
	}
	child := r.call(p, 57)
	kid := r.newestThread(p.TID())
	r.put(p, scratch, make([]byte, 128))
	if e := r.call(p, 247, pPID, uint64(child), scratch, wExited|wNoHang); e != 0 || le.Uint32(read(p, scratch+siPid, 4)) != 0 {
		t.Errorf("WNOHANG while the child runs = %d, si_pid %d", e, le.Uint32(read(p, scratch+siPid, 4)))
	}
	r.nu.Inject(proc.Request{TID: kid, Nr: 231, Args: [6]uint64{3}}) // exit_group(3)
	if e := r.call(p, 247, pPID, uint64(child), scratch, wExited|wNoWait); e != 0 {
		t.Fatalf("waitid WNOWAIT = %d", e)
	}
	info := read(p, scratch, 28)
	if le.Uint32(info[siSigno:]) != 17 || le.Uint32(info[siCode:]) != 1 || le.Uint32(info[siPid:]) != uint32(child) || le.Uint32(info[siSt:]) != 3 {
		t.Errorf("siginfo signo %d code %d pid %d status %d", le.Uint32(info[siSigno:]), le.Uint32(info[siCode:]),
			le.Uint32(info[siPid:]), le.Uint32(info[siSt:]))
	}
	if got := r.call(p, 61, uint64(child), scratch+200, 0); got != child { // WNOWAIT left it to be reaped
		t.Errorf("wait4 after WNOWAIT = %d", got)
	}
	if e := r.call(p, 247, 3, 5, scratch, wExited); e != -int64(syscall.EINVAL) { // P_PIDFD
		t.Errorf("P_PIDFD = %d", e)
	}
}
