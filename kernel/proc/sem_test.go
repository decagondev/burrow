package proc_test

import (
	"syscall"
	"testing"
	"time"

	"burrow/kernel/proc"
)

const (
	nrSemget, nrSemop, nrSemctl = 64, 65, 66
	semUndoFlag, ipcNowaitFlag  = 0x1000, 0o4000
	ipcCreatFlag, ipcExclFlag   = 0o1000, 0o2000
)

// sops stages struct sembuf operations at va.
func (r *rig) sops(p *proc.Process, va uint64, ops ...[3]int) uint64 {
	b := make([]byte, 6*len(ops))
	for i, o := range ops {
		le.PutUint16(b[6*i:], uint16(o[0]))
		le.PutUint16(b[6*i+2:], uint16(int16(o[1])))
		le.PutUint16(b[6*i+4:], uint16(int16(o[2])))
	}
	r.put(p, va, b)
	return uint64(len(ops))
}

func TestSemaphores(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	id := uint64(r.call(p, nrSemget, 0x414e, 2, ipcCreatFlag|ipcExclFlag|0o600))
	if int64(id) <= 0 {
		t.Fatalf("semget = %d", int64(id))
	}
	if e := r.call(p, nrSemget, 0x414e, 2, ipcCreatFlag|ipcExclFlag); e != -int64(syscall.EEXIST) {
		t.Errorf("semget again with IPC_EXCL = %d", e)
	}
	if again := r.call(p, nrSemget, 0x414e, 0, 0); uint64(again) != id {
		t.Errorf("semget of the key = %d, want %d", again, id)
	}
	if e := r.call(p, nrSemget, 0x999, 1, 0); e != -int64(syscall.ENOENT) {
		t.Errorf("semget of an unknown key = %d", e)
	}
	n := r.sops(p, scratch, [3]int{1, -1, ipcNowaitFlag})
	if e := r.call(p, nrSemop, id, scratch, n); e != -int64(syscall.EAGAIN) {
		t.Errorf("down on 0 with IPC_NOWAIT = %d", e)
	}
	if e := r.call(p, nrSemctl, id, 1, 16, 2); e != 0 { // SETVAL 2
		t.Fatalf("SETVAL = %d", e)
	}
	n = r.sops(p, scratch, [3]int{0, 0, 0}, [3]int{1, -2, 0}) // wait for 0 on #0 and take 2 from #1, together
	if e := r.call(p, nrSemop, id, scratch, n); e != 0 {
		t.Fatalf("semop = %d", e)
	}
	if v := r.call(p, nrSemctl, id, 1, 12, 0); v != 0 { // GETVAL
		t.Errorf("GETVAL = %d", v)
	}
	if pid := r.call(p, nrSemctl, id, 1, 11, 0); pid != int64(p.PID) { // GETPID
		t.Errorf("GETPID = %d", pid)
	}
	n = r.sops(p, scratch+128, [3]int{1, -1, 0}) // staged before the fork: the child's copy has it
	r.call(p, 57)                                // fork: the child waits on #1, the parent then raises it
	kid := r.newestThread(p.TID())
	r.nu.Inject(proc.Request{TID: kid, Nr: nrSemop, Args: [6]uint64{id, scratch + 128, n}})
	time.Sleep(20 * time.Millisecond) // it waits
	r.sops(p, scratch+64, [3]int{1, 3, 0})
	if e := r.call(p, nrSemop, id, scratch+64, 1); e != 0 {
		t.Fatalf("up = %d", e)
	}
	if e := r.await(kid); e != 0 {
		t.Fatalf("the waiting child's semop = %d", e)
	}
	if v := r.call(p, nrSemctl, id, 1, 12, 0); v != 2 {
		t.Errorf("after up 3 and down 1: %d", v)
	}
}

func TestSemUndoAndRemoval(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	id := uint64(r.call(p, nrSemget, 0, 1, ipcCreatFlag|0o600)) // IPC_PRIVATE
	n := r.sops(p, scratch, [3]int{0, 5, semUndoFlag})          // staged before the fork
	r.call(p, 57)
	kid := r.newestThread(p.TID())
	if e := r.callAs(kid, nrSemop, id, scratch, n); e != 0 {
		t.Fatalf("up with SEM_UNDO = %d", e)
	}
	r.nu.Inject(proc.Request{TID: kid, Nr: 231}) // the child exits: its 5 go back
	if pid := r.call(p, 61, ^uint64(0), 0, 0, 0); pid <= 0 {
		t.Fatalf("wait4 = %d", pid)
	}
	if v := r.call(p, nrSemctl, id, 0, 12, 0); v != 0 {
		t.Errorf("after the child's exit: %d, want 0", v)
	}
	n = r.sops(p, scratch, [3]int{0, -1, 0})
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: nrSemop, Args: [6]uint64{id, scratch, n}})
	time.Sleep(20 * time.Millisecond)
	q := r.spawn()
	if e := r.call(q, nrSemctl, id, 0, 0, 0); e != 0 { // IPC_RMID
		t.Fatalf("IPC_RMID = %d", e)
	}
	if e := r.await(p.TID()); e != -int64(syscall.EIDRM) {
		t.Errorf("a waiter on a removed set = %d", e)
	}
}
