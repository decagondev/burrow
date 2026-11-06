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
	"strings"
	"time"

	"burrow/kernel/arch/amd64"
	"burrow/kernel/bootinfo"
	"burrow/kernel/console"
	"burrow/kernel/driver"
	_ "burrow/kernel/drivers/ac97" // drivers register themselves in init()
	_ "burrow/kernel/drivers/ata"
	_ "burrow/kernel/drivers/bga"
	_ "burrow/kernel/drivers/e1000"
	_ "burrow/kernel/drivers/pci"
	_ "burrow/kernel/drivers/ps2"
	_ "burrow/kernel/drivers/rtc"
	"burrow/kernel/drivers/uart"
	_ "burrow/kernel/drivers/virtioblk"
	_ "burrow/kernel/drivers/virtioinput"
	_ "burrow/kernel/drivers/virtionet"
	"burrow/kernel/hal"
	"burrow/kernel/klog"
	"burrow/kernel/netstack"
	"burrow/kernel/proc"
	"burrow/kernel/random"
	"burrow/kernel/selftest"
	"burrow/kernel/tty"
	"burrow/kernel/vfs"
	"burrow/kernel/vt"
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
	env     []string  // added to every program's environment (a Qt disk's qt.env)
	con     *console.Console
	net     *netstack.Stack // TCP/IP (W17-S02); nil if it did not start
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
	mods, firmwareFB, err := bootModules(log.With("boot"))
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
	hooks := registerInput(dev, k.boot)
	keys := &vtKeys{vt: vt.New(), evdev: hooks.scancodes} // the console's modes (W14-S08)
	lines := driver.NewLines(func(line int) hal.IRQLine { return amd64.NewIRQ(line) }, log.With("driver"))
	driver.StartAll(&driver.Env{
		Ports:       amd64.Ports{},
		Ports16:     amd64.Ports{},
		Ports32:     amd64.Ports{},
		IRQ:         func(line int) hal.IRQLine { return amd64.NewIRQ(line) },
		Lines:       lines,
		Log:         log,
		Input:       input,
		Dev:         dev,
		Scancodes:   keys.Feed,
		Mouse:       hooks.mouse,
		Tablet:      hooks.tablet,
		MapMMIO:     amd64.MapMMIO,
		MapMMIOWC:   amd64.MapMMIOWC,
		DMAPage:     dmaPage,
		SetClock:    amd64.SetRealtime,
		Framebuffer: firmwareFB,
	})
	for _, l := range strings.Split(strings.TrimSpace(lines.String()), "\n") {
		if l != "" {
			log.With("driver").Infof("%s", l) // who serves each shared line (W22-S01)
		}
	}
	k.startNetwork(log)
	checkBootDisk(log.With("block"))
	diskTest(log.With("disktest"))
	diskBench(log.With("diskbench"))
	k.mountData(log) // each disk logs as its file system: [fat] or [ext2]

	rows, cols := 25, 80
	if screen := startScreen(); screen != nil {
		so := &screenOut{con: screen}
		out.Add(so) // the console on serial and on screen
		cols, rows = screen.Size()
		keys.vt.Graphics = so.holdGraphics
		if err := registerFB0(dev, so); err != nil {
			log.Warnf("/dev/fb0: %v", err)
		}
	}
	k.procs = proc.NewSystem(amd64.Nucleus{}, k.fs, out, log.With("proc"))
	k.mountProc(log.With("procfs"))
	k.tty, k.input = tty.New(out, rows, cols), input
	k.procs.SetTerminal(k.tty)
	k.procs.SetVT(keys.vt)
	if k.net != nil {
		k.procs.SetNetwork(k.net.Stack) // AF_INET sockets (W17-S03)
	}
	keys.term.Store(k.tty)
	dump := func() { k.procs.Dump(out) } // SysRq (W20-S01): every process and thread, for a hang
	keys.sysrq.Store(&dump)
	go k.procs.Serve() // the one dispatcher for every user syscall (W9)
	con := console.New(out, input)
	k.con = con
	addCommands(con, k)
	if loginAtBoot() { // W19-S06: someone logs in first; after they log out, the console
		log.With("console").Infof("login: /bin/login on the console")
		runProgram(k, []string{"/bin/login"}, out)
	}
	log.With("console").Infof("ready: type on the keyboard or the serial line (help lists commands)")
	con.Run()
}
