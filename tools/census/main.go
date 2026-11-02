// census is the workload for the Go runtime syscall census
// (docs/01-research/GO_RUNTIME_SYSCALL_CENSUS.md): goroutines, a channel,
// GC pressure with a sliding window, a timer, and a print. Run it on the
// host under strace to learn which Linux syscalls the pinned Go needs:
//
//	CGO_ENABLED=0 go build -o census ./census
//	GODEBUG=asyncpreemptoff=1 strace -f -c ./census
package main

import (
	"fmt"
	"runtime"
	"time"
)

func main() {
	done := make(chan int)
	go func() { done <- 42 }()
	v := <-done

	var window [16][]byte
	for i := 0; i < 2000; i++ {
		window[i%len(window)] = make([]byte, 64<<10)
	}
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	fmt.Println("census ok", v, len(window[0]))
}
