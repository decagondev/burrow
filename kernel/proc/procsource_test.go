package proc_test

import (
	"io/fs"
	"strconv"
	"strings"
	"testing"

	"burrow/kernel/procfs"
)

func TestProcSnapshot(t *testing.T) {
	r := newRig(t)
	p, err := r.sys.Spawn("bin/prog", []string{"prog", "-x", "two words"}, []string{"A=1", "HOME=/"})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := r.sys.Process(p.PID)
	if !ok {
		t.Fatal("no snapshot")
	}
	if s.Comm != "prog" || s.PPID != 1 || s.State != 'R' || s.Threads != 1 || s.Kernel ||
		strings.Join(s.Cmdline, "|") != "prog|-x|two words" || strings.Join(s.Environ, "|") != "A=1|HOME=/" {
		t.Fatalf("snapshot = %+v", s)
	}
	if s.SizeBytes < s.RSSBytes || s.RSSBytes == 0 || len(s.Files) != 3 || s.Files[2].Target != "/dev/console" {
		t.Errorf("size %d rss %d fds %+v", s.SizeBytes, s.RSSBytes, s.Files)
	}
	if s.Exe != "/bin/prog" || s.Cwd != "/" || len(s.Maps) < 2 || s.Maps[0].Name != "/bin/prog" || s.Maps[len(s.Maps)-1].Name != "[stack]" {
		t.Errorf("exe %q cwd %q maps %+v", s.Exe, s.Cwd, s.Maps)
	}
	if init, ok := r.sys.Process(1); !ok || !init.Kernel || init.Comm != "init" || init.State != 'S' {
		t.Errorf("init = %+v", init)
	}
	pids := r.sys.Pids()
	if len(pids) != 2 || pids[0] != 1 || pids[1] != p.PID {
		t.Errorf("pids = %v", pids)
	}
	child := int(r.call(p, 57)) // fork: the same command line
	if c, ok := r.sys.Process(child); !ok || strings.Join(c.Cmdline, "|") != "prog|-x|two words" || c.PPID != p.PID {
		t.Errorf("child = %+v", c)
	}
	if _, ok := r.sys.Process(9999); ok {
		t.Error("a pid nobody has")
	}
}

func TestProcFSOverSystem(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	f := procfs.New(r.sys, procfs.Static{CPUs: 1})
	pid := strconv.Itoa(p.PID)
	b, err := fs.ReadFile(f, pid+"/cmdline")
	if err != nil || string(b) != "/bin/prog\x00" {
		t.Fatalf("cmdline = %q, %v", b, err)
	}
	st, err := fs.ReadFile(f, pid+"/stat")
	if err != nil || !strings.HasPrefix(string(st), pid+" (prog) R 1 "+pid+" "+pid+" 1025 ") {
		t.Fatalf("stat = %q, %v", st, err)
	}
	if v, _ := fs.ReadFile(f, "sys/kernel/osrelease"); string(v) != "0.10\n" {
		t.Errorf("osrelease = %q", v)
	}
}
