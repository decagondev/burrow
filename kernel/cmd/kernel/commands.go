//go:build burrow

package main

import (
	"fmt"
	"io"
	"runtime"
	"time"

	"burrow/kernel/arch/amd64"
	"burrow/kernel/console"
	"burrow/kernel/drivers/pci"
)

const mib = 1 << 20

// addCommands gives the console the commands that need the kernel's state.
func addCommands(con *console.Console, k *kernel) {
	log := k.log.With("console")
	for _, cmd := range console.FileCommands(k.fs) {
		con.Add(cmd)
	}
	con.Add(console.Command{Name: "ps", Help: "list processes",
		Run: func(_ []string, out io.Writer) {
			fmt.Fprintln(out, "  PID  PPID  PGID STATE    COMMAND")
			for _, p := range k.procs.Processes() {
				fmt.Fprintf(out, "%5d %5d %5d %-8s %s\n", p.PID, p.PPID, p.PGID, p.State, p.Path)
			}
		}})
	con.Add(console.Command{Name: "sh", Help: "start the shell (/bin/sh) on this terminal; exit returns here",
		Run: func(args []string, out io.Writer) { runProgram(k, append([]string{"/bin/sh"}, args...), out) }})
	con.Add(console.Command{Name: "run", Help: "run <program> [args]: start a user program and wait for it",
		Run: func(args []string, out io.Writer) { runProgram(k, args, out) }})
	con.Add(console.Command{Name: "disks", Help: "list block devices",
		Run: func(_ []string, out io.Writer) { listDisks(out) }})
	con.Add(console.Command{Name: "modules", Help: "list the loader's boot modules",
		Run: func(_ []string, out io.Writer) { io.WriteString(out, listModules(k.modules)) }})
	con.Add(console.Command{Name: "lspci", Help: "list PCI functions found at boot",
		Run: func(_ []string, out io.Writer) {
			for _, d := range pci.Devices() {
				fmt.Fprintln(out, d)
			}
		}})
	con.Add(console.Command{Name: "uptime", Help: "time since the Go kernel started",
		Run: func(_ []string, out io.Writer) {
			fmt.Fprintf(out, "up %v\n", time.Since(k.boot).Round(time.Millisecond))
		}})
	con.Add(console.Command{Name: "mem", Help: "Go heap and GC numbers",
		Run: func(_ []string, out io.Writer) {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			fmt.Fprintf(out, "heap %d MiB in use, %d MiB from the nucleus, %d GC cycles, %d goroutines\n",
				m.HeapInuse/mib, m.Sys/mib, m.NumGC, runtime.NumGoroutine())
		}})
	con.Add(console.Command{Name: "poweroff", Help: "end the QEMU run",
		Run: func(_ []string, out io.Writer) {
			log.Infof("poweroff")
			amd64.DebugExit(0x10)
			fmt.Fprintln(out, "no debug-exit device here (make run-go): quit QEMU with Ctrl-A X")
		}})
}
