//go:build burrow

// Command kernel is the Go half of Burrow. It is built as an ordinary static
// linux/amd64 executable:
//
//	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags burrow \
//	    -ldflags='-T <text-addr> -s' -o build/kernel.elf ./cmd/kernel
//
// The nucleus (C) starts it exactly like Linux would start a process: it
// builds argc/argv/envp/auxv on a stack and jumps to the ELF entry point.
// Every system call the Go runtime makes lands in the nucleus syscall shim
// (see ADR-0003, docs/02-architecture/GO_RUNTIME_HOSTING.md).
//
// Boot: banner over COM1, the runtime self-tests (selftest), every
// registered driver (package driver; drivers register in init()), then the
// console on the keyboard and COM1 until `poweroff`.
package main

import (
	"fmt"
	"io"
	"time"

	"burrow/kernel/arch/amd64"
	"burrow/kernel/bootinfo"
	"burrow/kernel/console"
	"burrow/kernel/driver"
	_ "burrow/kernel/drivers/ata" // drivers register themselves in init()
	_ "burrow/kernel/drivers/bga"
	_ "burrow/kernel/drivers/pci"
	_ "burrow/kernel/drivers/ps2"
	"burrow/kernel/drivers/uart"
	"burrow/kernel/hal"
	"burrow/kernel/klog"
	"burrow/kernel/proc"
	"burrow/kernel/random"
	"burrow/kernel/selftest"
	"burrow/kernel/tty"
	"burrow/kernel/vfs"
)

// kernel is the state console commands and later subsystems share.
type kernel struct {
	boot    time.Time
	log     *klog.Logger
	modules []bootinfo.Module
	fs      *vfs.VFS
	procs   *proc.System
	tty     *tty.TTY  // the terminal user programs read and write
	input   chan byte // keyboard and serial bytes
	con     *console.Console
}

func main() {
	k := &kernel{boot: time.Now()}
	com1 := uart.New(amd64.Ports{}, uart.COM1)
	com1.Init()
	log := klog.New(com1, klog.Info)
	k.log = log
	log.Infof("hello from Go %s", "(Burrow kernel)")
	testFaults()
	seed, hw := amd64.Seed()
	random.Kernel.Seed(seed)
	if hw {
		log.With("random").Infof("seeded from RDRAND")
	} else {
		log.With("random").Infof("seeded from the TSC (no RDRAND)")
	}
	mods, err := bootModules(log.With("boot"))
	if err != nil {
		log.Errorf("boot data: %v", err)
		amd64.DebugExit(0x11)
	}
	k.modules = mods
	out := &consoleOut{ws: []io.Writer{log.Writer()}}
	dev := k.mountFiles(out)

	done := make(chan int)
	go func() { done <- 42 }()
	fmt.Fprintf(com1, "BURROW:GO:OK goroutine says %d\n", <-done)

	err = selftest.Run(com1, selftest.Kernel())
	if err == nil {
		err = selftest.RunChecks(com1, irqCheck())
	}
	if err != nil {
		log.Infof("selftest FAILED: %v", err)
		amd64.DebugExit(0x11)
	}
	fmt.Fprintln(com1, "BURROW:GO:SELFTEST:OK")

	input := make(chan byte, 256)
	driver.Register(uart.NewInput(com1, "serial", uart.COM1IRQ))
	scancodes, mouse := registerInput(dev, k.boot)
	driver.StartAll(&driver.Env{
		Ports:     amd64.Ports{},
		Ports16:   amd64.Ports{},
		Ports32:   amd64.Ports{},
		IRQ:       func(line int) hal.IRQLine { return amd64.NewIRQ(line) },
		Log:       log,
		Input:     input,
		Dev:       dev,
		Scancodes: scancodes,
		Mouse:     mouse,
		MapMMIO:   amd64.MapMMIO,
	})
	checkBootDisk(log.With("block"))
	diskTest(log.With("disktest"))
	k.mountData(log.With("fat"))

	rows, cols := 25, 80
	if screen := startScreen(); screen != nil {
		so := &screenOut{con: screen}
		out.Add(so) // the console on serial and on screen
		cols, rows = screen.Size()
		if err := registerFB0(dev, so); err != nil {
			log.Warnf("/dev/fb0: %v", err)
		}
	}
	k.procs = proc.NewSystem(amd64.Nucleus{}, k.fs, out, log.With("proc"))
	k.tty, k.input = tty.New(out, rows, cols), input
	k.procs.SetTerminal(k.tty)
	go k.procs.Serve() // the one dispatcher for every user syscall (W9)
	con := console.New(out, input)
	k.con = con
	addCommands(con, k)
	log.With("console").Infof("ready: type on the keyboard or the serial line (help lists commands)")
	con.Run()
}
