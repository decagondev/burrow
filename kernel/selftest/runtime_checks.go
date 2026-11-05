package selftest

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type shape interface{ area() float64 }
type rect struct{ w, h float64 }
type square struct{ s float64 }

func (r rect) area() float64   { return r.w * r.h }
func (s square) area() float64 { return s.s * s.s }

// checkLanguage: features that need the runtime (maps, growing slices,
// interfaces, closures, defer/recover, select with a timer).
func checkLanguage(Config) (string, error) {
	m := map[string]int{}
	for i := 0; i < 1000; i++ {
		m[fmt.Sprintf("k%d", i)] = i
	}
	if len(m) != 1000 || m["k777"] != 777 {
		return "", errors.New("map lost entries")
	}
	var s []int
	for i := 0; i < 10000; i++ {
		s = append(s, i)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(s)))
	if s[0] != 9999 || s[9999] != 0 {
		return "", errors.New("slice append/sort wrong")
	}
	total := 0.0
	for _, sh := range []shape{rect{2, 3}, square{4}} {
		total += sh.area()
	}
	if total != 22 {
		return "", fmt.Errorf("interfaces: total area %v, want 22", total)
	}
	counter := func() func() int { n := 0; return func() int { n++; return n } }()
	counter()
	if counter() != 2 {
		return "", errors.New("closure state lost")
	}
	if got := recovered(); got != "boom" {
		return "", fmt.Errorf("recover got %q", got)
	}
	select {
	case <-make(chan int):
		return "", errors.New("receive from a never-sent channel")
	case <-time.After(5 * time.Millisecond):
	}
	return strings.Join([]string{"map", "slice", "interface", "closure", "recover", "select"}, " "), nil
}

func recovered() (msg string) {
	defer func() { msg = fmt.Sprint(recover()) }()
	panic("boom")
}

// checkPingPong: two goroutines hand a counter back and forth over
// unbuffered channels; every hand-off is a park/ready in the scheduler.
func checkPingPong(cfg Config) (string, error) {
	ping, pong := make(chan int), make(chan int)
	go func() {
		for v := range ping {
			pong <- v + 1
		}
		close(pong)
	}()
	start := time.Now()
	v := 0
	for i := 0; i < cfg.PingPongs; i++ {
		ping <- v
		v = <-pong
	}
	close(ping)
	if v != cfg.PingPongs {
		return "", fmt.Errorf("counter %d, want %d", v, cfg.PingPongs)
	}
	return fmt.Sprintf("%d round trips in %v", cfg.PingPongs, time.Since(start).Round(time.Millisecond)), nil
}

// checkSync: many goroutines contending on a mutex, joined by a WaitGroup.
func checkSync(Config) (string, error) {
	const workers, each = 32, 1000
	var mu sync.Mutex
	var wg sync.WaitGroup
	sum := 0
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				mu.Lock()
				sum++
				mu.Unlock()
				if i%100 == 0 {
					runtime.Gosched()
				}
			}
		}()
	}
	wg.Wait()
	if sum != workers*each {
		return "", fmt.Errorf("sum %d, want %d", sum, workers*each)
	}
	return fmt.Sprintf("%d goroutines x %d locked increments", workers, each), nil
}

// checkGC: allocate GCTotal bytes through a sliding window of GCWindow live
// bytes. The heap must stay bounded (the GC really frees) and must have run.
func checkGC(cfg Config) (string, error) {
	const chunk = 64 << 10
	window := make([][]byte, cfg.GCWindow/chunk)
	var before, ms runtime.MemStats
	runtime.ReadMemStats(&before)
	var peak uint64
	for i := int64(0); i < cfg.GCTotal/chunk; i++ {
		b := make([]byte, chunk)
		b[0], b[chunk-1] = byte(i), byte(i>>8)
		window[i%int64(len(window))] = b
		if i%256 == 0 {
			runtime.ReadMemStats(&ms)
			peak = max(peak, ms.HeapSys)
			if ms.HeapSys > cfg.HeapLimit {
				return "", fmt.Errorf("heap grew to %d MiB (limit %d MiB)", ms.HeapSys/mib, cfg.HeapLimit/mib)
			}
		}
	}
	for i, b := range window { // the live window is intact
		if b == nil || len(b) != chunk {
			return "", fmt.Errorf("window slot %d lost", i)
		}
	}
	runtime.ReadMemStats(&ms)
	gcs := ms.NumGC - before.NumGC
	if gcs < 10 {
		return "", fmt.Errorf("only %d GC cycles for %d MiB", gcs, cfg.GCTotal/mib)
	}
	return fmt.Sprintf("%d MiB allocated, %d GC cycles, peak heap %d MiB, %d MiB from the OS, %d MiB released",
		cfg.GCTotal/mib, gcs, peak/mib, ms.Sys/mib, ms.HeapReleased/mib), nil
}

// checkParallel (W20-S08): the runtime has a P per CPU the nucleus reports,
// and that many goroutines really run at once: each spins until all have
// arrived, which on fewer CPUs than Ps could only happen by preemption,
// and the runtime's asynchronous preemption is off (GODEBUG), so it would
// time out.
func checkParallel(Config) (string, error) {
	n := runtime.GOMAXPROCS(0)
	if n != runtime.NumCPU() {
		return "", fmt.Errorf("GOMAXPROCS %d, but %d CPUs", n, runtime.NumCPU())
	}
	var arrived atomic.Int32
	deadline := time.Now().Add(5 * time.Second)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			arrived.Add(1)
			for arrived.Load() < int32(n) {
				if time.Now().After(deadline) {
					errs <- fmt.Errorf("only %d of %d goroutines ran at once", arrived.Load(), n)
					return
				}
			}
			errs <- nil
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%d Ps, %d running at once", n, n), nil
}
