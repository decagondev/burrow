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
	"time"

	"burrow/kernel/arch/amd64"
	"burrow/kernel/bootinfo"
	"burrow/kernel/console"
	"burrow/kernel/driver"
	_ "burrow/kernel/drivers/pci" // drivers register themselves in init()
	_ "burrow/kernel/drivers/ps2"
	"burrow/kernel/drivers/uart"
	"burrow/kernel/hal"
	"burrow/kernel/klog"
	"burrow/kernel/selftest"
)

// kernel is the state console commands and later subsystems share.
type kernel struct {
	boot    time.Time
	log     *klog.Logger
	modules []bootinfo.Module
}

func main() {
	k := &kernel{boot: time.Now()}
	com1 := uart.New(amd64.Ports{}, uart.COM1)
	com1.Init()
	log := klog.New(com1, klog.Info)
	k.log = log
	log.Infof("hello from Go %s", "(Burrow kernel)")
	testFaults()
	mods, err := bootModules(log.With("boot"))
	if err != nil {
		log.Errorf("boot data: %v", err)
		amd64.DebugExit(0x11)
	}
	k.modules = mods

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
	driver.StartAll(&driver.Env{
		Ports:   amd64.Ports{},
		Ports32: amd64.Ports{},
		IRQ:     func(line int) hal.IRQLine { return amd64.NewIRQ(line) },
		Log:     log,
		Input:   input,
	})

	con := console.New(log.Writer(), input)
	addCommands(con, k)
	log.With("console").Infof("ready: type on the keyboard or the serial line (help lists commands)")
	con.Run()
}
