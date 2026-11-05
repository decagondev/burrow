// Command gocpubench is cpubench's Go twin (W20-S08): four goroutines run
// the same CPU-bound loop on their own data, and the program prints the
// wall time with the runtime's GOMAXPROCS, which it sets from the CPUs
// Burrow's sched_getaffinity reports. make smp-bench runs it on one CPU and
// on four (tests/smoke/smp-budgets.py). It never needs anything but the
// Linux system calls Burrow serves.
// Docs: docs/04-plan/waves/WAVE-20-smp.md.
package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
)

const (
	workers = 4
	rounds  = 120_000_000
)

func work(k uint64) uint64 {
	x := 0x9E3779B97F4A7C15 ^ k
	for i := 0; i < rounds; i++ { // xorshift64
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
	}
	return x
}

func main() {
	var wg sync.WaitGroup
	results := make([]uint64, workers)
	t0 := time.Now()
	for k := 0; k < workers; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			results[k] = work(uint64(k))
		}(k)
	}
	wg.Wait()
	ms := time.Since(t0).Milliseconds()
	for k := 1; k < workers; k++ {
		if results[k] == results[0] {
			fmt.Println("gocpubench: FAIL goroutines computed the same")
			os.Exit(1)
		}
	}
	fmt.Printf("gocpubench: %d goroutines x %d rounds in %d ms with GOMAXPROCS %d\n",
		workers, rounds, ms, runtime.GOMAXPROCS(0))
}
