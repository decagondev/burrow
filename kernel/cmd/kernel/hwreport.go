//go:build burrow

// hwreport.go - the hardware report (W27-S05): what the kernel found, as
// text a tester sends back from a real PC: the firmware, the CPUs, the
// interrupt controller, every PCI function with its driver or "no driver",
// the USB devices, the disks, the mounts and every interrupt line's count.
// It is written to /var/log/hw.txt once the disks are mounted (to
// /tmp/hw.txt when the root cannot be written), printed by the console's
// `hw`, and printed at boot when the kargs say `hwreport`: on a real PC the
// screen is the only output. Never fails the boot. Docs:
// docs/06-guides/REAL_HARDWARE.md, docs/04-plan/waves/WAVE-27-real-pc.md (S05).
package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"burrow/kernel/arch/amd64"
	"burrow/kernel/bootinfo"
	"burrow/kernel/driver"
	"burrow/kernel/drivers/pci"
	"burrow/kernel/klog"
	"burrow/kernel/usb"
)

// hwReportAtBoot says whether the kargs ask for the report on the screen.
func hwReportAtBoot() bool {
	for _, a := range os.Args[1:] {
		if a == "hwreport" {
			return true
		}
	}
	return false
}

// hwReport is the report's text.
func (k *kernel) hwReport() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Burrow hardware report, %s\n", time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	firmware := "BIOS"
	if info, err := bootinfo.Parse(amd64.BootInfo()); err == nil && info.RSDP != 0 {
		firmware = "UEFI"
	}
	ctl := "8259 (virtual wire)"
	if r, ok := irqRoute(0); ok && r.Kind == driver.RouteIOAPIC {
		ctl = "I/O APIC"
	}
	fmt.Fprintf(&b, "firmware %s, %d CPUs online, interrupts through the %s\n", firmware, runtime.NumCPU(), ctl)
	b.WriteString("pci:\n")
	for _, d := range pci.Devices() {
		drv := "no driver"
		if c, ok := pci.Claimed(d); ok {
			drv = c.Driver + ", " + c.Detail
		}
		fmt.Fprintf(&b, "  %v: %s\n", d, drv)
	}
	b.WriteString("usb:\n")
	notes := usb.Notes()
	if len(notes) == 0 {
		b.WriteString("  none found\n")
	}
	for _, n := range notes {
		fmt.Fprintf(&b, "  %s\n", n)
	}
	b.WriteString("disks:\n")
	listDisks(indent{&b})
	b.WriteString("mounts:\n")
	for _, m := range k.fs.Mounts() {
		mode := "read-write"
		if m.ReadOnly {
			mode = "read-only"
		}
		fmt.Fprintf(&b, "  /%s: %s, %s\n", strings.TrimPrefix(m.Point, "/"), m.Kind, mode)
	}
	b.WriteString("interrupts:\n")
	listIRQs(k, indent{&b})
	return b.String()
}

// indent puts two spaces before every line written through it.
type indent struct{ w io.Writer }

func (i indent) Write(p []byte) (int, error) {
	lines := strings.SplitAfter(string(p), "\n")
	for _, l := range lines {
		if l != "" {
			io.WriteString(i.w, "  "+l)
		}
	}
	return len(p), nil
}

// writeHWReport saves the report and, with kargs hwreport, prints it.
func (k *kernel) writeHWReport(log *klog.Logger, out io.Writer) {
	r := k.hwReport()
	path := "var/log/hw.txt"
	k.fs.Mkdir("var") // there already, mostly
	k.fs.Mkdir("var/log")
	err := k.fs.WriteFile(path, []byte(r))
	if err != nil {
		path = "tmp/hw.txt"
		err = k.fs.WriteFile(path, []byte(r))
	}
	if err != nil {
		log.Warnf("report not written (%v); the console's hw prints it", err)
	} else {
		log.Infof("report in /%s (%d lines; the console's hw prints it)", path, strings.Count(r, "\n"))
	}
	if hwReportAtBoot() {
		fmt.Fprint(out, r)
	}
}
