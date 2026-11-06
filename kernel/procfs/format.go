package procfs

// format.go - the files' contents, field for field as proc(5) gives them:
// a pid's stat (52 fields), statm, status, cmdline, environ and comm, and
// the system's uptime, loadavg, meminfo, version and filesystems. CPU time
// is zero until W23-S02 counts it. Docs: WAVE-23-proc-virtio.md.

import (
	"fmt"
	"io/fs"
	"strings"
	"time"
)

// pidFiles are the files in every /proc/<pid>/.
var pidFiles = []struct {
	name string
	mode fs.FileMode
	gen  func(f *FS, p *Proc) []byte
}{
	{"cmdline", fileMode, func(_ *FS, p *Proc) []byte { return nulList(p.Cmdline) }},
	{"comm", fileMode, func(_ *FS, p *Proc) []byte { return []byte(p.Comm + "\n") }},
	{"environ", 0o400, func(_ *FS, p *Proc) []byte { return nulList(p.Environ) }},
	{"stat", fileMode, func(_ *FS, p *Proc) []byte { return []byte(statLine(p)) }},
	{"statm", fileMode, func(_ *FS, p *Proc) []byte { return []byte(statm(p)) }},
	{"status", fileMode, func(f *FS, p *Proc) []byte { return []byte(status(p, f.st.CPUs)) }},
}

// systemFiles are the files at the root.
var systemFiles = []struct {
	name string
	gen  func(f *FS) []byte
}{
	{"cpuinfo", func(f *FS) []byte { return []byte(cpuinfo(f.st.CPU, f.st.CPUs)) }},
	{"filesystems", func(*FS) []byte { return []byte(filesystems) }},
	{"loadavg", func(f *FS) []byte { return []byte(loadavg(f.src.Load())) }},
	{"meminfo", func(f *FS) []byte { return []byte(meminfo(f.src.Memory())) }},
	{"uptime", func(f *FS) []byte { return []byte(uptime(f.src.Uptime(), f.st.CPUs)) }},
	{"version", func(f *FS) []byte { return []byte(version(f.src.Uname())) }},
}

// kernelFiles are sys/kernel's, each one line.
var kernelFiles = []struct {
	name string
	gen  func(f *FS) string
}{
	{"hostname", func(f *FS) string { return f.src.Uname().Node }},
	{"domainname", func(f *FS) string { return f.src.Uname().Domain }},
	{"ostype", func(f *FS) string { return f.src.Uname().Sys }},
	{"osrelease", func(f *FS) string { return f.src.Uname().Release }},
	{"version", func(f *FS) string { return f.src.Uname().Version }},
	{"pid_max", func(*FS) string { return fmt.Sprint(pidMax) }},
}

// the kinds of file system the VFS mounts (the mount table's names)
const filesystems = "nodev\ttarfs\nnodev\tramfs\nnodev\tdevfs\nnodev\tproc\n\tfat32\n\text2\n"

// nulList is each string followed by a NUL (cmdline, environ).
func nulList(ss []string) []byte {
	var b []byte
	for _, s := range ss {
		b = append(append(b, s...), 0)
	}
	return b
}

func ticks(d time.Duration) uint64 { return uint64(d / (time.Second / clockTicks)) }

// statLine is /proc/<pid>/stat: pid (comm) state, then 49 numbers.
func statLine(p *Proc) string {
	rss := p.RSSBytes / pageBytes
	exitSignal := 17 // SIGCHLD
	return fmt.Sprintf("%d (%s) %c %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d\n",
		p.PID, p.Comm, p.State, p.PPID, p.PGID, p.SID, p.TTY, p.TPGID,
		0,          // flags
		0, 0, 0, 0, // minflt cminflt majflt cmajflt
		0, 0, 0, 0, // utime stime cutime cstime (W23-S02)
		20, 0, // priority nice
		p.Threads, 0, ticks(p.Start), p.SizeBytes, rss,
		^uint64(0),    // rsslim
		0, 0, 0, 0, 0, // startcode endcode startstack kstkesp kstkeip
		p.SigPending, p.SigBlocked, p.SigIgnored, p.SigCaught,
		0, 0, 0, exitSignal, // wchan nswap cnswap exit_signal
		0, 0, 0, 0, // processor rt_priority policy delayacct_blkio_ticks
		0, 0, // guest_time cguest_time
		0, 0, p.BrkStart, // start_data end_data start_brk
		0, 0, 0, 0, // arg_start arg_end env_start env_end
		0) // exit_code
}

// statm is /proc/<pid>/statm, in pages: size resident shared text lib data dt.
func statm(p *Proc) string {
	return fmt.Sprintf("%d %d 0 0 0 0 0\n", p.SizeBytes/pageBytes, p.RSSBytes/pageBytes)
}

var stateNames = map[byte]string{'R': "R (running)", 'S': "S (sleeping)", 'Z': "Z (zombie)"}

// status is /proc/<pid>/status: "Key:\tvalue" lines; a zombie and the
// kernel's init have no memory lines, as on Linux.
func status(p *Proc, cpus int) string {
	var b strings.Builder
	line := func(k, format string, v ...any) { fmt.Fprintf(&b, "%s:\t"+format+"\n", append([]any{k}, v...)...) }
	line("Name", "%s", p.Comm)
	line("Umask", "%04o", p.Umask)
	line("State", "%s", stateNames[p.State])
	line("Tgid", "%d", p.PID)
	line("Ngid", "0")
	line("Pid", "%d", p.PID)
	line("PPid", "%d", p.PPID)
	line("TracerPid", "0")
	line("Uid", "%d\t%d\t%d\t%d", p.UID[0], p.UID[1], p.UID[2], p.UID[3])
	line("Gid", "%d\t%d\t%d\t%d", p.GID[0], p.GID[1], p.GID[2], p.GID[3])
	line("FDSize", "%d", max(64, p.FDs))
	var groups strings.Builder
	for _, g := range p.Groups {
		fmt.Fprintf(&groups, "%d ", g)
	}
	line("Groups", "%s", groups.String())
	if p.State != 'Z' && !p.Kernel {
		line("VmPeak", "%8d kB", p.SizeBytes>>10)
		line("VmSize", "%8d kB", p.SizeBytes>>10)
		line("VmHWM", "%8d kB", p.RSSBytes>>10)
		line("VmRSS", "%8d kB", p.RSSBytes>>10)
	}
	line("Threads", "%d", p.Threads)
	line("SigQ", "0/%d", sigQueueMax)
	line("SigPnd", "%016x", p.SigPending)
	line("ShdPnd", "%016x", p.SigPending)
	line("SigBlk", "%016x", p.SigBlocked)
	line("SigIgn", "%016x", p.SigIgnored)
	line("SigCgt", "%016x", p.SigCaught)
	line("Cpus_allowed_list", "0-%d", max(0, cpus-1))
	return b.String()
}

const sigQueueMax = 7823 // RLIMIT_SIGPENDING's usual value

func uptime(up time.Duration, cpus int) string {
	idle := 0.0 // the idle threads' time is counted from W23-S02
	return fmt.Sprintf("%.2f %.2f\n", up.Seconds(), idle*float64(cpus))
}

func loadavg(l Load) string {
	return fmt.Sprintf("%.2f %.2f %.2f %d/%d %d\n", l.Avg[0], l.Avg[1], l.Avg[2], l.Running, l.Threads, l.LastPID)
}

// meminfo is /proc/meminfo; there is no swap and no page cache.
func meminfo(m Memory) string {
	var b strings.Builder
	for _, kv := range []struct {
		k string
		v uint64
	}{
		{"MemTotal", m.Total}, {"MemFree", m.Free}, {"MemAvailable", m.Free},
		{"Buffers", 0}, {"Cached", 0}, {"SwapCached", 0}, {"Active", 0}, {"Inactive", 0},
		{"SwapTotal", 0}, {"SwapFree", 0}, {"Dirty", 0}, {"Writeback", 0},
		{"AnonPages", m.Total - m.Free - m.Shared}, {"Mapped", 0}, {"Shmem", m.Shared},
	} {
		fmt.Fprintf(&b, "%-16s%8d kB\n", kv.k+":", kv.v>>10)
	}
	return b.String()
}

func version(u Uname) string {
	return fmt.Sprintf("%s version %s (burrow@%s) %s\n", u.Sys, u.Release, u.Node, u.Version)
}
