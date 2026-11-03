package proc_test

import (
	"syscall"
	"testing"

	"burrow/kernel/proc"
	"burrow/kernel/tty"
	"burrow/kernel/vt"
)

func vtRig(t *testing.T) (*rig, *vt.VT, *[]bool) {
	r, _ := vfsRig(t)
	r.sys.SetTerminal(tty.New(r.console, 30, 100))
	v := vt.New()
	var screen []bool
	v.Graphics = func(g bool) { screen = append(screen, g) }
	r.sys.SetVT(v)
	return r, v, &screen
}

func TestKDModes(t *testing.T) {
	r, v, screen := vtRig(t)
	p := r.spawn()
	if e := r.call(p, 16, 0, 0x4B3A, 1); e != 0 || v.KDMode() != vt.KDGraphics { // KDSETMODE KD_GRAPHICS on fd 0
		t.Fatalf("KDSETMODE = %d", e)
	}
	if e := r.call(p, 16, 0, 0x4B45, 4); e != 0 || v.KbMode() != vt.KOff { // KDSKBMODE K_OFF
		t.Fatalf("KDSKBMODE = %d", e)
	}
	if r.call(p, 16, 0, 0x4B44, scratch); le.Uint32(read(p, scratch, 4)) != vt.KOff {
		t.Error("KDGKBMODE")
	}
	if e := r.call(p, 16, 0, 0x4B45, 9); e != -int64(syscall.EINVAL) {
		t.Errorf("KDSKBMODE 9 = %d", e)
	}
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 60}) // exit: the console comes back
	wait(t, p)
	if v.KDMode() != vt.KDText || v.KbMode() != vt.KXlate || len(*screen) != 2 || (*screen)[1] {
		t.Errorf("after exit: display %d keyboard %d, screen calls %v", v.KDMode(), v.KbMode(), *screen)
	}
}

// A process that dies of a signal gives the console back too.
func TestKDModeCrash(t *testing.T) {
	r, v, _ := vtRig(t)
	p := r.spawn()
	r.call(p, 16, 0, 0x4B3A, 1)
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: proc.FaultBase | 14, Args: [6]uint64{0, 0x400200, 4}}) // a NULL read
	if st, _ := wait(t, p); st != 128+11 || v.KDMode() != vt.KDText {
		t.Errorf("status %d, display %d after a crash", st, v.KDMode())
	}
}

func TestVTCalls(t *testing.T) {
	r, _, _ := vtRig(t)
	p := r.spawn()
	fd := r.call(p, 2, r.str(p, scratch, "/dev/tty0"), 2) // O_RDWR
	if fd < 3 {
		t.Fatalf("open /dev/tty0 = %d", fd)
	}
	if r.call(p, 16, uint64(fd), 0x5600, scratch); le.Uint32(read(p, scratch, 4)) != 1 { // VT_OPENQRY
		t.Error("VT_OPENQRY")
	}
	if r.call(p, 16, uint64(fd), 0x5603, scratch); le.Uint16(read(p, scratch, 2)) != 1 || le.Uint16(read(p, scratch+4, 2)) != 2 {
		t.Errorf("VT_GETSTATE % x", read(p, scratch, 6))
	}
	if e := r.call(p, 16, uint64(fd), 0x5606, 1); e != 0 { // VT_ACTIVATE 1
		t.Errorf("VT_ACTIVATE 1 = %d", e)
	}
	if e := r.call(p, 16, uint64(fd), 0x5607, 2); e != -int64(syscall.ENXIO) {
		t.Errorf("VT_WAITACTIVE 2 = %d", e)
	}
	r.put(p, scratch, []byte{1, 0, 10, 0, 12, 0, 0, 0}) // VT_PROCESS, relsig 10, acqsig 12
	if e := r.call(p, 16, uint64(fd), 0x5602, scratch); e != 0 {
		t.Errorf("VT_SETMODE = %d", e)
	}
	if r.call(p, 16, uint64(fd), 0x5601, scratch+16); read(p, scratch+16, 1)[0] != 1 || le.Uint16(read(p, scratch+18, 2)) != 10 {
		t.Errorf("VT_GETMODE % x", read(p, scratch+16, 8))
	}
	if e := r.call(p, 16, uint64(fd), 0x4B33, scratch); e != 0 || read(p, scratch, 1)[0] != 2 { // KDGKBTYPE: KB_101
		t.Errorf("KDGKBTYPE = %d", e)
	}
}
