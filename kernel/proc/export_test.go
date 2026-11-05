// export_test.go - hooks the external tests (package proc_test) need into
// package proc's internals. Test builds only; never part of the kernel.
package proc

// SetNumCPU makes the affinity calls see n CPUs online (W20-S08) and
// returns a function that restores the real count.
func SetNumCPU(n int) (restore func()) {
	saved := numCPU
	numCPU = func() int { return n }
	return func() { numCPU = saved }
}
