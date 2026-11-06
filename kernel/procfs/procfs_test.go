package procfs

import (
	"errors"
	"io/fs"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"burrow/kernel/fs/memfs"
	"burrow/kernel/fsattr"
	"burrow/kernel/vfs"
)

// fake is a Source with two processes: init (kernel) and a shell.
type fake struct{ procs map[int]Proc }

func newFake() *fake {
	return &fake{procs: map[int]Proc{
		1: {PID: 1, PGID: 1, SID: 1, State: 'S', Comm: "init", Cmdline: []string{"init"}, Threads: 0, TPGID: -1, Kernel: true},
		7: {PID: 7, PPID: 1, PGID: 7, SID: 7, TTY: 0x401, TPGID: 7, State: 'R', Comm: "sh",
			Cmdline: []string{"/bin/sh", "-c", "echo hi"}, Environ: []string{"HOME=/", "PATH=/bin"}, Threads: 1,
			Start: 1500 * time.Millisecond, UID: [4]uint32{1000, 1000, 1000, 1000}, GID: [4]uint32{100, 100, 100, 100},
			Groups: []uint32{100, 10}, Umask: 0o022, SizeBytes: 8 << 20, RSSBytes: 64 << 10, SigBlocked: 1 << 1, BrkStart: 0x404000,
			User: 2500 * time.Millisecond, Sys: 300 * time.Millisecond, ChildUser: time.Second, ChildSys: 20 * time.Millisecond,
			Tasks: []Task{{TID: 7, State: 'R', User: 2 * time.Second, Sys: 300 * time.Millisecond}, {TID: 9, State: 'S', User: 500 * time.Millisecond}},
			Files: []FD{{Num: 0, Target: "/dev/tty1", Flags: 2}, {Num: 1, Target: "pipe:[42]", Flags: 1}, {Num: 3, Target: "/etc/motd", Pos: 6}},
			Exe:   "/bin/sh", Cwd: "/tmp",
			Maps: []Map{{Start: 0x400000, End: 0x401000, Perms: "r-xp", Name: "/bin/sh"}, {Start: 0x7fff0000, End: 0x7fff1000, Perms: "rw-p", Name: "[stack]"}}},
	}}
}

func (f *fake) Pids() []int { return []int{1, 7} }
func (f *fake) Process(pid int) (Proc, bool) {
	p, ok := f.procs[pid]
	return p, ok
}
func (f *fake) Uptime() time.Duration { return 125*time.Second + 340*time.Millisecond }
func (f *fake) Load() Load {
	return Load{Avg: [3]float64{0.5, 0.25, 0.125}, Running: 1, Threads: 2, LastPID: 7}
}
func (f *fake) Memory() Memory { return Memory{Total: 512 << 20, Free: 400 << 20, Shared: 1 << 20} }
func (f *fake) CPUs() []CPUTime {
	return []CPUTime{{User: 2 * time.Second, Sys: time.Second, Idle: 10 * time.Second, Switches: 40},
		{User: time.Second, Sys: 500 * time.Millisecond, Idle: 20 * time.Second, Switches: 2}}
}
func (f *fake) Mounts() []Mount {
	return []Mount{{"tarfs", "/", "tarfs", true}, {"ramfs", "/tmp", "ramfs", false}}
}

// MayTrace: a process traces itself; pid 1 (root) traces anyone.
func (f *fake) MayTrace(c vfs.Caller, pid int) bool { return c.PID == pid || c.PID == 1 }

func (f *fake) Uname() Uname {
	return Uname{Sys: "Burrow", Node: "burrow", Release: "0.10", Version: "#1 Wave 10", Domain: "(none)"}
}

func testFS() *FS {
	return New(newFake(), Static{CPUs: 4, Boot: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
		CPU: CPU{Vendor: "GenuineIntel", Name: "Test CPU", Family: 6, Model: 61, Flags: []string{"fpu", "sse2"}}})
}

func read(t *testing.T, f *FS, name string) string {
	t.Helper()
	b, err := fs.ReadFile(f, name)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return string(b)
}

func TestFSConformance(t *testing.T) {
	if err := fstest.TestFS(testFS(), "1", "7", "7/stat", "7/status", "7/task/9/stat", "stat", "uptime", "meminfo", "sys/kernel/ostype", "self"); err != nil {
		t.Fatal(err)
	}
}

func TestStatHas52Fields(t *testing.T) {
	line := strings.TrimSuffix(read(t, testFS(), "7/stat"), "\n")
	if !strings.HasPrefix(line, "7 (sh) R 1 7 7 1025 7 ") {
		t.Fatalf("stat = %q", line)
	}
	rest := line[strings.LastIndex(line, ")")+2:]
	if n := 2 + len(strings.Fields(rest)); n != 52 {
		t.Fatalf("%d fields, want 52: %q", n, line)
	}
	f := strings.Fields(rest)
	if f[19] != "150" || f[20] != "8388608" || f[21] != "16" { // starttime ticks, vsize bytes, rss pages
		t.Fatalf("starttime/vsize/rss = %s %s %s", f[19], f[20], f[21])
	}
	if strings.Join(f[11:15], " ") != "250 30 100 2" { // utime stime cutime cstime, in ticks
		t.Fatalf("times = %v", f[11:15])
	}
}

func TestStatusAndLists(t *testing.T) {
	f := testFS()
	st := read(t, f, "7/status")
	for _, want := range []string{"Name:\tsh\n", "Umask:\t0022\n", "State:\tR (running)\n", "PPid:\t1\n",
		"Uid:\t1000\t1000\t1000\t1000\n", "Groups:\t100 10 \n", "VmRSS:\t      64 kB\n", "SigBlk:\t0000000000000002\n",
		"Cpus_allowed_list:\t0-3\n"} {
		if !strings.Contains(st, want) {
			t.Errorf("status lacks %q:\n%s", want, st)
		}
	}
	if s := read(t, f, "1/status"); strings.Contains(s, "VmRSS") {
		t.Errorf("init has memory lines:\n%s", s)
	}
	if got := read(t, f, "7/cmdline"); got != "/bin/sh\x00-c\x00echo hi\x00" {
		t.Errorf("cmdline = %q", got)
	}
	info, err := f.Lstat("7/environ")
	if a, _ := fsattr.Of(info); err != nil || info.Mode().Perm() != 0o400 || a.UID != 1000 {
		t.Errorf("environ: %v %v %+v", err, info.Mode(), a)
	}
}

func TestSystemFiles(t *testing.T) {
	f := testFS()
	for name, want := range map[string]string{
		"uptime":            "125.34 30.00\n",
		"loadavg":           "0.50 0.25 0.12 1/2 7\n",
		"version":           "Burrow version 0.10 (burrow@burrow) #1 Wave 10\n",
		"sys/kernel/ostype": "Burrow\n",
	} {
		if got := read(t, f, name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	m := read(t, f, "meminfo")
	if !strings.HasPrefix(m, "MemTotal:         524288 kB\nMemFree:          409600 kB\n") || !strings.Contains(m, "Cached:") {
		t.Errorf("meminfo:\n%s", m)
	}
	if c := read(t, f, "cpuinfo"); strings.Count(c, "processor\t: ") != 4 || !strings.Contains(c, "flags\t\t: fpu sse2\n") {
		t.Errorf("cpuinfo:\n%s", c)
	}
}

func TestSelfIsTheCaller(t *testing.T) {
	f := testFS()
	if got, _ := f.ReadLinkAs("self", vfs.Caller{PID: 7, TID: 7}); got != "7" {
		t.Fatalf("self as 7 = %q", got)
	}
	if got, _ := f.ReadLink("self"); got != "1" {
		t.Fatalf("self as the kernel = %q", got)
	}
	if info, err := f.Lstat("self"); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("self: %v %v", info, err)
	}
	v := vfs.New()
	v.Mount(".", memfs.New(), "ramfs")
	v.Mount("proc", f, "proc")
	if got, err := v.ResolveAs("/proc/self/stat", true, vfs.Caller{PID: 7}, nil); err != nil || got != "proc/7/stat" {
		t.Fatalf("resolve = %q, %v", got, err)
	}
}

func TestGoneProcess(t *testing.T) {
	src := newFake()
	f := New(src, Static{})
	d, err := f.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	delete(src.procs, 7) // exits between the open and the listing
	entries, err := d.(fs.ReadDirFile).ReadDir(-1)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "7" {
			t.Fatal("a gone process is listed")
		}
	}
	if _, err := f.Open("7/stat"); err == nil {
		t.Fatal("a gone process opens")
	}
}

func TestDecodeCPU(t *testing.T) {
	leaves := map[uint32][4]uint32{
		0:          {0xd, 0x756e6547, 0x6c65746e, 0x49656e69}, // GenuineIntel
		1:          {0x306d2, 0, 1<<0 | 1<<9 | 1<<28 | 1<<31, 1<<0 | 1<<25 | 1<<26},
		7:          {0, 1<<5 | 1<<7 | 1<<20, 0, 0},
		0x80000000: {0x80000008, 0, 0, 0},
		0x80000001: {0, 0, 1, 1<<20 | 1<<29},
		0x80000002: {0x65746e49, 0x6f43206c, 0x50206572, 0x65636f72}, // "Intel Core Proce"
		0x80000003: {0x726f7373, 0, 0, 0},                            // "ssor"
		0x80000004: {0, 0, 0, 0},
		0x80000008: {0x3028, 0, 0, 0},
	}
	c := DecodeCPU(func(leaf, _ uint32) (a, b, cx, d uint32) {
		r := leaves[leaf]
		return r[0], r[1], r[2], r[3]
	})
	if c.Vendor != "GenuineIntel" || c.Name != "Intel Core Processor" || c.Family != 6 || c.Model != 61 || c.Step != 2 {
		t.Fatalf("cpu = %+v", c)
	}
	if got := strings.Join(c.Flags, " "); got != "fpu sse sse2 nx lm pni ssse3 avx hypervisor lahf_lm avx2 smep smap" {
		t.Fatalf("flags = %q", got)
	}
	if c.PhysBits != 40 || c.VirtBits != 48 {
		t.Fatalf("address bits %d %d", c.PhysBits, c.VirtBits)
	}
}

func TestStatAndTasks(t *testing.T) {
	f := testFS()
	want := "cpu  300 0 150 3000 0 0 0 0 0 0\ncpu0 200 0 100 1000 0 0 0 0 0 0\ncpu1 100 0 50 2000 0 0 0 0 0 0\n" +
		"intr 0\nctxt 42\nbtime 1893553445\nprocesses 7\nprocs_running 1\nprocs_blocked 0\nsoftirq 0\n"
	if got := read(t, f, "stat"); got != want {
		t.Errorf("stat =\n%s\nwant\n%s", got, want)
	}
	entries, err := fs.ReadDir(f, "7/task")
	if err != nil || len(entries) != 2 || entries[0].Name() != "7" || entries[1].Name() != "9" {
		t.Fatalf("task/ = %v, %v", entries, err)
	}
	st := read(t, f, "7/task/9/stat")
	if !strings.HasPrefix(st, "9 (sh) S 1 7 7 ") || strings.Fields(st)[13] != "50" {
		t.Errorf("task 9's stat = %q", st)
	}
	if s := read(t, f, "7/task/9/status"); !strings.Contains(s, "\nTgid:\t7\n") || !strings.Contains(s, "\nPid:\t9\n") {
		t.Errorf("task 9's status:\n%s", s)
	}
	if _, err := f.Open("7/task/8/stat"); err == nil {
		t.Error("a thread nobody has")
	}
}

func TestDescriptorsAndLinks(t *testing.T) {
	f := testFS()
	entries, err := fs.ReadDir(f, "7/fd")
	if err != nil || len(entries) != 3 || entries[2].Name() != "3" {
		t.Fatalf("fd/ = %v, %v", entries, err)
	}
	if info, _ := f.Lstat("7/fd"); info.Mode().Perm() != 0o500 {
		t.Errorf("fd/ mode %v", info.Mode())
	}
	for name, want := range map[string]string{"7/fd/0": "/dev/tty1", "7/fd/1": "pipe:[42]", "7/exe": "/bin/sh", "7/cwd": "/tmp", "7/root": "/"} {
		if got, err := f.ReadLinkAs(name, vfs.Caller{PID: 7}); err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", name, got, err, want)
		}
	}
	if _, err := f.ReadLinkAs("7/fd/0", vfs.Caller{PID: 99}); !errors.Is(err, syscall.EACCES) {
		t.Errorf("another user's fd/0: %v", err)
	}
	if got := read(t, f, "7/fdinfo/3"); got != "pos:\t6\nflags:\t00\nmnt_id:\t0\n" {
		t.Errorf("fdinfo/3 = %q", got)
	}
	m := read(t, f, "7/maps")
	if !strings.HasPrefix(m, "00400000-00401000 r-xp 00000000 00:00 0 ") || !strings.HasSuffix(m, " [stack]\n") {
		t.Errorf("maps =\n%s", m)
	}
	if got, _ := f.ReadLink("mounts"); got != "self/mounts" {
		t.Errorf("mounts -> %q", got)
	}
	if got := read(t, f, "7/mounts"); got != "tarfs / tarfs ro 0 0\nramfs /tmp ramfs rw 0 0\n" {
		t.Errorf("mounts = %q", got)
	}
}
