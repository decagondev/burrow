//go:build burrow

package main

// vtkeys.go - the console terminal's keyboard mode on the way from the
// PS/2 driver (W14-S08): every scancode still reaches evdev; in K_XLATE
// the console translates it as before, in K_RAW and K_MEDIUMRAW the
// terminal gets scancodes or key codes instead, in K_OFF it goes nowhere
// else. SysRq (W20-S01) is the kernel's own: it starts a dump of every
// process and thread (proc.System.Dump) and reaches nobody else. Runs on
// the keyboard's IRQ goroutine: no allocation beyond what vt.Key returns,
// never blocks. Docs: docs/04-plan/waves/WAVE-14-desktop-groundwork.md.

import (
	"sync/atomic"

	"burrow/kernel/tty"
	"burrow/kernel/vt"
)

type vtKeys struct {
	vt    *vt.VT
	evdev func(byte) bool         // evdev's Feed: true while a reader grabs the keyboard
	term  atomic.Pointer[tty.TTY] // set once the terminal exists
	sysrq atomic.Pointer[func()]  // the dump, set once the process system exists
}

// scSysRq is SysRq's make code in scancode set 1 (Alt+PrintScreen sends it
// too); its break code is the same with the top bit set.
const scSysRq = 0x54

// Feed is driver.Env.Scancodes: true means the console must not type it.
func (k *vtKeys) Feed(sc byte) bool {
	if sc&0x7F == scSysRq {
		if f := k.sysrq.Load(); f != nil && sc == scSysRq {
			go (*f)()
		}
		return true
	}
	grabbed := k.evdev(sc)
	console, raw := k.vt.Key(sc)
	if t := k.term.Load(); t != nil {
		for _, b := range raw {
			t.Input(b)
		}
	}
	return grabbed || !console
}
