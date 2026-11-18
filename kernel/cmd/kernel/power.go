//go:build burrow

// power.go - what restarting and turning the machine off do (W28-S04), for
// reboot(2), PID 1's signals and the console's reboot: a restart goes to
// the nucleus's reset (the FADT's reset register, then port 0xCF9, the
// keyboard controller and a triple fault; the console may start further
// down the list, so each way can be tried on a real PC and in
// reboot-smoke); a power-off is the console's poweroff, the debug-exit
// device under a QEMU test, else ACPI's S5. The log line comes first, so a
// real machine's screen says which was asked for before it goes away.
// Never returns from a restart.
package main

import (
	"fmt"
	"io"
	"slices"

	"burrow/kernel/arch/amd64"
	"burrow/kernel/proc"
)

// resetWays names the ways to restart, in the order the nucleus tries them.
var resetWays = []string{"register", "cf9", "kbc", "triple"}

func (k *kernel) power(cmd uint32) {
	log := k.log.With("power")
	if cmd == proc.RebootRestart {
		log.Infof("reboot")
		amd64.Reset(0)
		return
	}
	log.Infof("poweroff")
	amd64.DebugExit(0x10)
}

// reboot is the console's reboot [way].
func (k *kernel) reboot(args []string, out io.Writer) {
	from := 0
	if len(args) > 0 {
		if from = slices.Index(resetWays, args[0]); from < 0 {
			fmt.Fprintf(out, "reboot: ways are %v\n", resetWays)
			return
		}
	}
	k.log.With("power").Infof("reboot from %s", resetWays[from])
	amd64.Reset(from)
}
