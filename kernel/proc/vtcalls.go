package proc

// vtcalls.go - the console terminal's KD* and VT_* ioctls (W14-S08), over
// kernel/vt: the keyboard and display modes and one virtual terminal,
// number 1. /dev/tty, /dev/tty0 and /dev/tty1 all open the console
// terminal. A process's modes are undone when it exits (vt.Release), so
// a crashed X server never leaves the console dead. Never switches: there
// is no second terminal. Docs: POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"

	"burrow/kernel/vt"
)

// <linux/kd.h> and <linux/vt.h>
const (
	kdgetled   = 0x4B31
	kdsetled   = 0x4B32
	kdgkbtype  = 0x4B33
	kdsetmode  = 0x4B3A
	kdgetmode  = 0x4B3B
	kdgkbmode  = 0x4B44
	kdskbmode  = 0x4B45
	vtOpenqry  = 0x5600
	vtGetmode  = 0x5601
	vtSetmode  = 0x5602
	vtGetstate = 0x5603
	vtReldisp  = 0x5605
	vtActivate = 0x5606
	vtWaitact  = 0x5607

	kb101     = 2 // KDGKBTYPE: a PC keyboard
	theVT     = 1
	vtModeLen = 8 // struct vt_mode
)

// SetVT gives the console terminal its modes (the kernel hooks them to the
// screen and the keyboard).
func (s *System) SetVT(v *vt.VT) {
	s.mu.Lock()
	s.vt = v
	s.mu.Unlock()
}

// releaseVT undoes the modes p set (its exit).
func (s *System) releaseVT(pid int) {
	s.mu.Lock()
	v := s.vt
	s.mu.Unlock()
	if v != nil {
		v.Release(pid)
	}
}

// ttyByPath is the console terminal opened as /dev/tty, tty0 or tty1.
func (s *System) ttyByPath(name string) (*file, bool) {
	switch name {
	case "dev/tty", "dev/tty0", "dev/tty1":
	default:
		return nil, false
	}
	s.mu.Lock()
	t := s.tty
	s.mu.Unlock()
	if t == nil {
		return nil, false
	}
	f := newFile(ttyFile{t}, nil)
	f.path = name
	return f, true
}

// vtIoctl answers the KD* and VT_* requests on the terminal; handled is
// false for any other request.
func (p *Process) vtIoctl(req, arg uint64) (res int64, handled bool) {
	p.sys.mu.Lock()
	v := p.sys.vt
	p.sys.mu.Unlock()
	if v == nil {
		return 0, false
	}
	le := binary.LittleEndian
	putInt := func(n uint32) int64 {
		var b [4]byte
		le.PutUint32(b[:], n)
		return errno(p.mem.CopyOut(arg, b[:]))
	}
	switch req {
	case kdgkbmode:
		return putInt(uint32(v.KbMode())), true
	case kdskbmode:
		return errno(v.SetKbMode(p.PID, int(arg))), true
	case kdgetmode:
		return putInt(uint32(v.KDMode())), true
	case kdsetmode:
		return errno(v.SetKDMode(p.PID, int(arg))), true
	case kdgkbtype:
		return errno(p.mem.CopyOut(arg, []byte{kb101})), true
	case kdgetled:
		return errno(p.mem.CopyOut(arg, []byte{0})), true
	case kdsetled, vtReldisp:
		return 0, true // no LEDs to light; nothing to hand over
	case vtOpenqry:
		return putInt(theVT), true
	case vtGetstate: // struct vt_stat {active, signal, state}: terminal 1 in use
		b := make([]byte, 6)
		le.PutUint16(b[0:], theVT)
		le.PutUint16(b[4:], 1<<theVT)
		return errno(p.mem.CopyOut(arg, b)), true
	case vtActivate, vtWaitact:
		if arg != theVT {
			return -int64(syscall.ENXIO), true
		}
		return 0, true
	case vtGetmode, vtSetmode:
		return p.vtMode(v, req, arg), true
	}
	return 0, false
}

func (p *Process) vtMode(v *vt.VT, req, arg uint64) int64 {
	le := binary.LittleEndian
	b := make([]byte, vtModeLen)
	if req == vtGetmode {
		m := v.VTMode()
		b[0], b[1] = m.Mode, m.Waitv
		le.PutUint16(b[2:], uint16(m.Relsig))
		le.PutUint16(b[4:], uint16(m.Acqsig))
		le.PutUint16(b[6:], uint16(m.Frsig))
		return errno(p.mem.CopyOut(arg, b))
	}
	if err := p.mem.CopyIn(arg, b); err != nil {
		return errno(err)
	}
	return errno(v.SetVTMode(p.PID, vt.Mode{Mode: b[0], Waitv: b[1], Relsig: int16(le.Uint16(b[2:])),
		Acqsig: int16(le.Uint16(b[4:])), Frsig: int16(le.Uint16(b[6:]))}))
}
