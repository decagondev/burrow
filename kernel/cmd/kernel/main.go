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
// Boot: banner over COM1, the runtime self-tests (selftest), then every
// registered driver (package driver; drivers register in init()).
package main

import (
	"fmt"
	"time"

	"burrow/kernel/arch/amd64"
	"burrow/kernel/driver"
	_ "burrow/kernel/drivers/pci" // drivers register themselves in init()
	"burrow/kernel/drivers/uart"
	"burrow/kernel/hal"
	"burrow/kernel/klog"
	"burrow/kernel/selftest"
)

func main() {
	com1 := uart.New(amd64.Ports{}, uart.COM1)
	com1.Init()
	log := klog.New(com1, klog.Info)
	log.Infof("hello from Go %s", "(Burrow kernel)")

	done := make(chan int)
	go func() { done <- 42 }()
	fmt.Fprintf(com1, "BURROW:GO:OK goroutine says %d\n", <-done)

	err := selftest.Run(com1, selftest.Kernel())
	if err == nil {
		err = selftest.RunChecks(com1, irqCheck())
	}
	if err != nil {
		log.Infof("selftest FAILED: %v", err)
		amd64.DebugExit(0x11)
	}
	fmt.Fprintln(com1, "BURROW:GO:SELFTEST:OK")

	driver.StartAll(&driver.Env{
		Ports:   amd64.Ports{},
		Ports32: amd64.Ports{},
		IRQ:     func(line int) hal.IRQLine { return amd64.NewIRQ(line) },
		Log:     log,
	})
	amd64.DebugExit(0x10) // ends a QEMU test run; returns everywhere else
	for {
		time.Sleep(time.Hour) // idle: the nucleus halts the CPU while every thread sleeps
	}
}
