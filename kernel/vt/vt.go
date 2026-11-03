// Package vt is the console's one virtual terminal (W14-S08): the keyboard
// mode (KDSKBMODE), the display mode (KDSETMODE) and the switching mode
// (VT_SETMODE) that full-screen programs and X servers set before they
// draw. Each is remembered with the pid that set it, and Release puts the
// console back when that process exits, so a crashed program never leaves
// the console dead. Key decides what each keyboard scancode becomes in
// the current mode. It never touches hardware: the kernel hooks Graphics
// to the screen and feeds Key from the PS/2 driver.
// Reference: <linux/kd.h>, <linux/vt.h>, Linux drivers/tty/vt/keyboard.c.
package vt

import (
	"sync"
	"syscall"

	"burrow/kernel/evdev"
)

// Keyboard modes (KDSKBMODE) and display modes (KDSETMODE).
const (
	KRaw       = 0 // scancodes to the terminal
	KXlate     = 1 // the console translates keys (the default)
	KMediumRaw = 2 // key codes to the terminal
	KUnicode   = 3 // as KXlate here
	KOff       = 4 // keys go nowhere but evdev

	KDText     = 0
	KDGraphics = 1

	VTAuto    = 0
	VTProcess = 1

	releaseBit = 0x80
	prefixE0   = 0xE0
)

// Mode is struct vt_mode.
type Mode struct {
	Mode, Waitv           uint8
	Relsig, Acqsig, Frsig int16
}

// VT is safe for concurrent use.
type VT struct {
	mu                    sync.Mutex
	kb, kd                int
	mode                  Mode
	kbPID, kdPID, modePID int
	e0                    bool

	// Graphics, if set, runs when the display mode changes (true: a
	// program draws; the console must stand aside).
	Graphics func(graphics bool)
	// Keyboard, if set, runs when the keyboard mode changes: the terminal
	// drops its unread input then, as Linux's KDSKBMODE does (BUG-0020).
	Keyboard func()
}

func New() *VT { return &VT{kb: KXlate} }

func (v *VT) KbMode() int { v.mu.Lock(); defer v.mu.Unlock(); return v.kb }
func (v *VT) KDMode() int { v.mu.Lock(); defer v.mu.Unlock(); return v.kd }
func (v *VT) VTMode() Mode {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.mode
}

// SetKbMode sets the keyboard mode for process pid.
func (v *VT) SetKbMode(pid, m int) error {
	if m < KRaw || m > KOff {
		return syscall.EINVAL
	}
	v.mu.Lock()
	changed := v.kb != m
	v.kb, v.kbPID, v.e0 = m, pid, false
	hook := v.Keyboard
	v.mu.Unlock()
	if changed && hook != nil {
		hook()
	}
	return nil
}

// SetKDMode sets the display mode for process pid.
func (v *VT) SetKDMode(pid, m int) error {
	if m != KDText && m != KDGraphics {
		return syscall.EINVAL
	}
	v.mu.Lock()
	changed := v.kd != m
	v.kd, v.kdPID = m, pid
	hook := v.Graphics
	v.mu.Unlock()
	if changed && hook != nil {
		hook(m == KDGraphics)
	}
	return nil
}

// SetVTMode sets the switching mode for process pid (with one terminal
// there is never a switch to ask about, so VT_PROCESS changes nothing).
func (v *VT) SetVTMode(pid int, m Mode) error {
	if m.Mode != VTAuto && m.Mode != VTProcess {
		return syscall.EINVAL
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.mode, v.modePID = m, pid
	return nil
}

// Release undoes every mode process pid set (its exit).
func (v *VT) Release(pid int) {
	v.mu.Lock()
	keyboard := v.kbPID == pid && v.kb != KXlate
	if v.kbPID == pid {
		v.kb, v.kbPID, v.e0 = KXlate, 0, false
	}
	if v.modePID == pid {
		v.mode, v.modePID = Mode{}, 0
	}
	graphics := v.kdPID == pid && v.kd == KDGraphics
	if v.kdPID == pid {
		v.kd, v.kdPID = KDText, 0
	}
	hook, kbHook := v.Graphics, v.Keyboard
	v.mu.Unlock()
	if graphics && hook != nil {
		hook(false)
	}
	if keyboard && kbHook != nil {
		kbHook()
	}
}

// Key takes one scancode (set 1) from the keyboard: console says the
// console translates it as usual; raw is what the terminal gets instead
// (K_RAW the scancode, K_MEDIUMRAW Linux's key code bytes, K_OFF nothing).
func (v *VT) Key(sc byte) (console bool, raw []byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	switch v.kb {
	case KXlate, KUnicode:
		return true, nil
	case KRaw:
		return false, []byte{sc}
	case KOff:
		return false, nil
	}
	if sc == prefixE0 {
		v.e0 = true
		return false, nil
	}
	code := evdev.KeyCode(v.e0, sc&^releaseBit)
	v.e0 = false
	up := sc & releaseBit
	if code == 0 {
		return false, nil
	}
	if code < 128 {
		return false, []byte{byte(code) | up}
	}
	return false, []byte{up, releaseBit | byte(code>>7), releaseBit | byte(code&0x7F)}
}
