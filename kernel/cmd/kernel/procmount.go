//go:build burrow

package main

// procmount.go - /proc (W23-S01): kernel/procfs over the process table,
// mounted once the System exists, with the CPU as CPUID describes it. Never
// mounted before the processes it lists. Docs: WAVE-23-proc-virtio.md.

import (
	"runtime"
	"time"

	"burrow/kernel/arch/amd64"
	"burrow/kernel/klog"
	"burrow/kernel/procfs"
)

func (k *kernel) mountProc(log *klog.Logger) {
	st := procfs.Static{CPU: procfs.DecodeCPU(amd64.CPUID), CPUs: runtime.NumCPU(), Boot: time.Now()}
	if err := k.fs.Mount("proc", procfs.New(k.procs, st), "proc"); err != nil {
		log.Errorf("/proc: %v", err)
		return
	}
	log.Infof("/proc mounted: %d CPUs, %s", st.CPUs, st.CPU.Name)
}
