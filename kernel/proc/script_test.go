package proc_test

import (
	"syscall"
	"testing"

	"burrow/kernel/proc"
)

// argvAfterExec reads the new program's argv off its stack.
func argvAfterExec(t *testing.T, r *rig, p *proc.Process) []string {
	t.Helper()
	th := r.nu.Thread(p.TID())
	mem := p.Mem()
	b := make([]byte, 8)
	mem.CopyIn(th.Regs.RSP, b)
	var argv []string
	argc := le.Uint64(b)
	for i := uint64(0); i < argc; i++ {
		mem.CopyIn(th.Regs.RSP+8+8*i, b)
		s, _ := mem.CString(le.Uint64(b), 256)
		argv = append(argv, s)
	}
	return argv
}

// execve of a "#!" script runs its interpreter with the line's one
// argument, the script's path and the rest of argv, as Linux does.
func TestExecveScript(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.put(p, scratch, []byte("bin/script\x00x\x00"))
	argv := make([]byte, 24)
	le.PutUint64(argv, scratch)
	le.PutUint64(argv[8:], scratch+11)
	r.put(p, scratch+64, argv)
	if e := r.call(p, 59, scratch, scratch+64, 0); e != 0 {
		t.Fatalf("execve of a script = %d", e)
	}
	got := argvAfterExec(t, r, p)
	want := []string{"/bin/prog", "-a b", "bin/script", "x"}
	if len(got) != len(want) {
		t.Fatalf("argv %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("argv %q, want %q", got, want)
			break
		}
	}
	if p.Path != "/bin/prog" {
		t.Errorf("the process runs %q, want the interpreter", p.Path)
	}
	r.put(p, scratch, []byte("bin/nested\x00"))
	if e := r.call(p, 59, scratch, 0, 0); e != -int64(syscall.ENOEXEC) {
		t.Errorf("a script whose interpreter is a script = %d, want ENOEXEC", e)
	}
	r.put(p, scratch, []byte("etc/text\x00"))
	if e := r.call(p, 59, scratch, 0, 0); e != -int64(syscall.ENOENT) {
		t.Errorf("a script whose interpreter is missing = %d, want ENOENT", e)
	}
}
