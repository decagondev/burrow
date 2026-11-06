// Command gohello is a Go program running as a Burrow user process
// (W12-S03): a static GOOS=linux binary built by the host's Go toolchain,
// unmodified. It exercises what the Go runtime asks of the kernel: threads
// (clone, futex, tgkill), timers (epoll, eventfd), the garbage collector
// (mmap, madvise) and files. It prints "gohello: ok" when every check holds
// and must never need anything but the Linux system calls Burrow serves.
// Docs: docs/04-plan/waves/WAVE-12-ports.md.
package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

func main() {
	bad := false
	check := func(ok bool, format string, a ...any) {
		fmt.Printf("gohello: "+format+"\n", a...)
		bad = bad || !ok
	}
	fmt.Printf("gohello: %s on %s/%s, pid %d\n", runtime.Version(), runtime.GOOS, runtime.GOARCH, os.Getpid())

	// goroutines and channels: a pipeline of workers
	jobs, results := make(chan int), make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				results <- j * j
			}
		}()
	}
	go func() {
		for i := 1; i <= 100; i++ {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	sum := 0
	for r := range results {
		sum += r
	}
	check(sum == 338350, "8 goroutines squared 1..100: %d (want 338350)", sum)

	// a mutex under contention from many goroutines
	var mu sync.Mutex
	count := 0
	var all sync.WaitGroup
	for g := 0; g < 50; g++ {
		all.Add(1)
		go func() {
			defer all.Done()
			for i := 0; i < 1000; i++ {
				mu.Lock()
				count++
				mu.Unlock()
				if i%100 == 0 {
					runtime.Gosched()
				}
			}
		}()
	}
	all.Wait()
	check(count == 50000, "50 goroutines counted %d (want 50000)", count)

	// timers: the runtime sleeps in its network poller
	start := time.Now()
	time.Sleep(30 * time.Millisecond)
	tick := time.NewTicker(5 * time.Millisecond)
	for i := 0; i < 3; i++ {
		<-tick.C
	}
	tick.Stop()
	slept := time.Since(start)
	check(slept >= 45*time.Millisecond && slept < 5*time.Second, "timers: slept and ticked for %v", slept.Round(time.Millisecond))

	// the garbage collector: allocate far more than stays live
	var keep [][]byte
	for i := 0; i < 400; i++ {
		b := make([]byte, 64<<10)
		b[0] = byte(i)
		if i%40 == 0 {
			keep = append(keep, b)
		}
	}
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	check(m.NumGC > 0 && len(keep) == 10, "GC: %d cycles, %d MiB allocated in all", m.NumGC, m.TotalAlloc>>20)

	// files through os
	exe, err := os.Executable() // /proc/self/exe (W23-S03)
	check(err == nil && strings.HasSuffix(exe, "/gohello"), "executable %q (%v)", exe, err)
	motd, err := os.ReadFile("/etc/motd")
	check(err == nil && len(motd) > 0, "read /etc/motd: %d bytes (%v)", len(motd), err)
	host, _ := os.Hostname()
	ents, err := os.ReadDir("/bin")
	check(err == nil && len(ents) > 5, "hostname %q, %d programs in /bin", strings.TrimSpace(host), len(ents))

	if bad {
		fmt.Println("gohello: FAILED")
		os.Exit(1)
	}
	fmt.Println("gohello: ok")
}
