package proc_test

import (
	"syscall"
	"testing"
	"time"

	"burrow/kernel/proc"
	"burrow/kernel/tty"
)

const pollIn, pollOut, pollHup, pollNval = 0x1, 0x4, 0x10, 0x20

// pollfds stages struct pollfd {fd, events, revents} entries at va.
func (r *rig) pollfds(p *proc.Process, va uint64, fds ...[2]int) {
	b := make([]byte, 8*len(fds))
	for i, f := range fds {
		le.PutUint32(b[8*i:], uint32(int32(f[0])))
		le.PutUint16(b[8*i+4:], uint16(f[1]))
	}
	r.put(p, va, b)
}

func revents(p *proc.Process, va uint64, i int) uint16 {
	b := make([]byte, 2)
	p.Mem().CopyIn(va+uint64(8*i)+6, b)
	return le.Uint16(b)
}

func TestPollPipe(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.call(p, 22, scratch) // pipe: 3 and 4
	fds := uint64(scratch + 256)
	r.pollfds(p, fds, [2]int{3, pollIn}, [2]int{4, pollOut}, [2]int{-1, pollIn}, [2]int{50, pollIn})
	if n := r.call(p, 7, fds, 4, 0); n != 2 || revents(p, fds, 0) != 0 || revents(p, fds, 1) != pollOut ||
		revents(p, fds, 2) != 0 || revents(p, fds, 3) != pollNval {
		t.Errorf("empty pipe: %d ready, revents %#x %#x %#x %#x", n,
			revents(p, fds, 0), revents(p, fds, 1), revents(p, fds, 2), revents(p, fds, 3))
	}
	r.call(p, 1, 4, scratch, 2)
	if n := r.call(p, 7, fds, 1, 0); n != 1 || revents(p, fds, 0) != pollIn {
		t.Errorf("with data: %d ready, revents %#x", n, revents(p, fds, 0))
	}
	r.call(p, 3, 4)
	if r.call(p, 7, fds, 1, 0); revents(p, fds, 0) != pollIn|pollHup {
		t.Errorf("writer gone: revents %#x, want POLLIN|POLLHUP", revents(p, fds, 0))
	}
}

func TestPollTimeoutAndPpoll(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.call(p, 22, scratch)
	fds := uint64(scratch + 256)
	r.pollfds(p, fds, [2]int{3, pollIn})
	start := time.Now()
	if n := r.call(p, 7, fds, 1, 30); n != 0 || time.Since(start) < 30*time.Millisecond {
		t.Errorf("poll timeout: %d after %v", n, time.Since(start))
	}
	ts := make([]byte, 16)
	le.PutUint64(ts[8:], 20e6) // 20 ms
	r.put(p, scratch+512, ts)
	start = time.Now()
	if n := r.call(p, 271, fds, 1, scratch+512, 0, 8); n != 0 || time.Since(start) < 20*time.Millisecond {
		t.Errorf("ppoll timeout: %d after %v", n, time.Since(start))
	}
}

// A poll on the terminal waits for a line and counts as a reader meanwhile
// (the console forwards keys only to a program that waits for them).
func TestPollTerminal(t *testing.T) {
	r := newRig(t)
	term := tty.New(r.console, 30, 100)
	r.sys.SetTerminal(term)
	p := r.spawn()
	fds := uint64(scratch + 256)
	r.pollfds(p, fds, [2]int{0, pollIn})
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 7, Args: [6]uint64{fds, 1, ^uint64(0)}})
	r.waitFor(term.HasReader)
	term.Input('a')
	time.Sleep(10 * time.Millisecond) // canonical: not ready before the newline
	if !term.HasReader() {
		t.Fatal("poll returned before a whole line")
	}
	term.Input('\r')
	if n := r.await(p.TID()); n != 1 || revents(p, fds, 0) != pollIn {
		t.Errorf("poll = %d, revents %#x", n, revents(p, fds, 0))
	}
	if term.HasReader() {
		t.Error("still counted as a reader after poll returned")
	}
}

func TestPollInterrupted(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.install(p, sigINT, saRestart) // poll is never restarted (Linux)
	r.call(p, 22, scratch)
	regs := r.running(p)
	regs.RIP = 0x400262
	r.nu.SetRegs(p.TID(), regs)
	fds := uint64(scratch + 256)
	r.pollfds(p, fds, [2]int{3, pollIn})
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 7, Args: [6]uint64{fds, 1, ^uint64(0)}})
	time.Sleep(10 * time.Millisecond)
	r.sys.Kill(int64(p.PID), sigINT)
	r.await(p.TID())
	h := r.nu.Snapshot(p.TID()).Regs
	uc := make([]byte, 304)
	p.Mem().CopyIn(h.RDX, uc)
	if h.RIP != handlerVA || int64(le.Uint64(uc[40+13*8:])) != -int64(syscall.EINTR) {
		t.Errorf("rip %#x, saved rax %d: want the handler and -EINTR", h.RIP, int64(le.Uint64(uc[40+13*8:])))
	}
}

func TestPrctlName(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.str(p, scratch, "worker")
	if r.call(p, 157, 15, scratch) != 0 || r.call(p, 157, 16, scratch+64) != 0 {
		t.Fatal("PR_SET_NAME / PR_GET_NAME")
	}
	b := make([]byte, 16)
	p.Mem().CopyIn(scratch+64, b)
	if string(b[:5]) != "prog\x00" {
		t.Errorf("PR_GET_NAME %q", b)
	}
	if e := r.call(p, 157, 999); e != -int64(syscall.EINVAL) {
		t.Errorf("unknown option = %d", e)
	}
}
