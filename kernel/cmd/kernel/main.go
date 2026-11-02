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
// WAVE 6 target: this prints the banner below over COM1, then goroutines,
// channels and the GC are exercised by the selftest package.
package main

import (
	"fmt"

	"burrow/kernel/arch/amd64"
	"burrow/kernel/drivers/uart"
	"burrow/kernel/klog"
)

func main() {
	com1 := uart.New(amd64.Ports{}, uart.COM1)
	com1.Init()
	log := klog.New(com1, klog.Info)
	log.Infof("hello from Go %s", "(Burrow kernel)")

	done := make(chan int)
	go func() { done <- 42 }()
	fmt.Fprintf(com1, "BURROW:GO:OK goroutine says %d\n", <-done)
	amd64.DebugExit(0x10) // ends a QEMU test run; returns everywhere else
	select {}             // the nucleus scheduler idles the CPU when every thread blocks
}
