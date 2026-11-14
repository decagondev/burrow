//go:build burrow

// irqs.go - the console's `irqs` (W27-S01): where each ISA line goes (the
// 8259 or an I/O APIC pin, its trigger and polarity), then every line a PCI
// driver serves with its route, the interrupts it served and its drivers.
// Read-only; the hardware report of W27-S05 prints the same. Docs: ADR-0014.
package main

import (
	"fmt"
	"io"

	"burrow/kernel/arch/amd64"
	"burrow/kernel/driver"
)

func irqRoute(line int) (driver.Route, bool) {
	v, err := amd64.IRQRoute(line)
	if err != nil {
		return driver.Route{}, false
	}
	return driver.ParseRoute(v), true
}

func listIRQs(k *kernel, out io.Writer) {
	for n := 0; n < 16; n++ {
		if r, ok := irqRoute(n); ok {
			fmt.Fprintf(out, "isa line %d: %v\n", n, r)
		}
	}
	if k.lines != nil {
		io.WriteString(out, k.lines.Report(irqRoute))
	}
}
