// Package procfs is /proc (W23-S01): an io/fs.FS whose files are generated
// when they are opened, from a snapshot of the process table that a Source
// gives, and formatted exactly as Linux's proc(5) says, so busybox, musl and
// Go read them unchanged. It never imports kernel/proc (kernel/proc's
// System implements Source) and never holds a lock of its own: each open is
// one snapshot. Docs: docs/04-plan/waves/WAVE-23-proc-virtio.md.
package procfs

import "time"

// Source is what /proc shows: the process table and the system's numbers.
type Source interface {
	Pids() []int                  // live processes and zombies, ascending
	Process(pid int) (Proc, bool) // a snapshot of one; false once it is gone
	Uptime() time.Duration        // since boot
	Load() Load
	Memory() Memory
	Uname() Uname
	CPUs() []CPUTime // each online CPU's time (W23-S02)
}

// Proc is one process as /proc/<pid>/ shows it.
type Proc struct {
	PID, PPID, PGID, SID int
	TTY                  int  // the controlling terminal's device number (major<<8 | minor)
	TPGID                int  // its foreground process group, -1 for none
	State                byte // 'R' running, 'S' sleeping (in a system call), 'Z' zombie
	Comm                 string
	Cmdline, Environ     []string
	Threads              int
	Start                time.Duration // since boot
	UID, GID             [4]uint32     // real, effective, saved, file system
	Groups               []uint32
	Umask                uint32
	SizeBytes, RSSBytes  uint64 // address space reserved, and pages present
	SigPending           uint64 // process-directed, and the main thread's
	SigBlocked           uint64 // the main thread's mask
	SigIgnored           uint64
	SigCaught            uint64
	BrkStart, Brk        uint64
	FDs                  int // open descriptors (status's FDSize is a capacity)
	Kernel               bool
	User, Sys            time.Duration // CPU time (W23-S02)
	ChildUser, ChildSys  time.Duration // its reaped children's
	Tasks                []Task        // its threads (task/)
	TGID                 int           // in a task's view, the process's pid (0: PID)
}

// Task is one thread of a process.
type Task struct {
	TID       int
	State     byte
	User, Sys time.Duration
}

// Load is /proc/loadavg: the 1, 5 and 15 minute averages of runnable
// threads, the threads runnable now and in all, and the last pid given.
type Load struct {
	Avg              [3]float64
	Running, Threads int
	LastPID          int
}

// Memory is RAM in bytes.
type Memory struct{ Total, Free, Shared uint64 }

// Uname is what uname(2) says, which /proc/version and sys/kernel repeat.
type Uname struct{ Sys, Node, Release, Version, Domain string }

// Static is what /proc knows from boot: the CPUs and when it started.
type Static struct {
	CPU  CPU
	CPUs int
	Boot time.Time // the modification time of every file
}

const (
	clockTicks = 100  // USER_HZ: what stat's times count in
	pageBytes  = 4096 // rss and statm count pages
	pidMax     = 4194304
)
