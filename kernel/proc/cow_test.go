package proc_test

import (
	"testing"

	"burrow/kernel/proc"
	"burrow/kernel/proc/proctest"
)

const cowVA = 0x400000

// cowMem is a process memory with two written pages at cowVA.
func cowMem(t *testing.T, nu *proctest.Fake) *proc.Mem {
	t.Helper()
	m, err := proc.NewMem(nu)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.MapAnon(cowVA, 2*proc.PageSize, proc.ProtWrite); err != nil {
		t.Fatal(err)
	}
	m.CopyOut(cowVA, []byte("parent page 0"))
	m.CopyOut(cowVA+proc.PageSize, []byte("parent page 1"))
	return m
}

func str(t *testing.T, m *proc.Mem, va uint64, n int) string {
	t.Helper()
	b := make([]byte, n)
	if err := m.CopyIn(va, b); err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func pa(t *testing.T, nu *proctest.Fake, m *proc.Mem, va uint64) uint64 {
	t.Helper()
	p, ok := nu.PA(m.Space(), va)
	if !ok {
		t.Fatalf("%#x not mapped", va)
	}
	return p
}

func writable(nu *proctest.Fake, m *proc.Mem, va uint64) bool {
	p, _ := nu.Prot(m.Space(), va)
	return p&proc.ProtWrite != 0
}

func TestCowForkSharesThenCopiesOnWrite(t *testing.T) {
	nu := proctest.New()
	m := cowMem(t, nu)
	before := nu.FramesInUse()
	c, err := m.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if nu.FramesInUse() != before {
		t.Fatalf("fork took %d frames, want none", nu.FramesInUse()-before)
	}
	if pa(t, nu, m, cowVA) != pa(t, nu, c, cowVA) || writable(nu, m, cowVA) || writable(nu, c, cowVA) {
		t.Fatal("after fork the page should be one frame, read-only in both")
	}
	if err := c.CopyOut(cowVA, []byte("child ")); err != nil { // the kernel writing (read(2) into a buffer)
		t.Fatal(err)
	}
	if str(t, m, cowVA, 13) != "parent page 0" || str(t, c, cowVA, 13) != "child  page 0" {
		t.Fatalf("parent %q, child %q", str(t, m, cowVA, 13), str(t, c, cowVA, 13))
	}
	if pa(t, nu, m, cowVA) == pa(t, nu, c, cowVA) || !writable(nu, c, cowVA) || nu.FramesInUse() != before+1 {
		t.Fatal("the child's write should have copied exactly one page")
	}
	if pa(t, nu, m, cowVA+proc.PageSize) != pa(t, nu, c, cowVA+proc.PageSize) {
		t.Fatal("the page nobody wrote should still be shared")
	}
	// the parent is now the frame's last owner: a write fault takes it back, no copy
	old := pa(t, nu, m, cowVA)
	if f := m.FaultIn(cowVA+8, true); f != proc.FaultResume {
		t.Fatalf("write fault = %v, want resume", f)
	}
	if pa(t, nu, m, cowVA) != old || !writable(nu, m, cowVA) || nu.FramesInUse() != before+1 {
		t.Fatal("the last owner's write fault should reuse its frame")
	}
	if f := m.FaultIn(cowVA+proc.PageSize, true); f != proc.FaultResume || str(t, c, cowVA+proc.PageSize, 13) != "parent page 1" {
		t.Fatal("a write fault on a shared page should copy it for the writer")
	}
}

func TestCowThreeOwnersAnyExitOrder(t *testing.T) {
	nu := proctest.New()
	base := nu.FramesInUse()
	m := cowMem(t, nu)
	c1, _ := m.Clone()
	c2, _ := c1.Clone()
	if nu.FramesInUse() != base+2 {
		t.Fatalf("%d frames in use, want 2", nu.FramesInUse()-base)
	}
	c1.CopyOut(cowVA, []byte("c1"))
	m.Close()
	if str(t, c2, cowVA, 13) != "parent page 0" || str(t, c1, cowVA, 2) != "c1" {
		t.Fatal("after the parent exits its children keep their bytes")
	}
	c1.Close()
	if str(t, c2, cowVA+proc.PageSize, 13) != "parent page 1" {
		t.Fatal("the last child lost a page")
	}
	c2.Close()
	if nu.FramesInUse() != base {
		t.Fatalf("%d frames leaked", nu.FramesInUse()-base)
	}
}

func TestCowZeroKeepsTheSiblingsPage(t *testing.T) {
	nu := proctest.New()
	m := cowMem(t, nu)
	c, _ := m.Clone()
	m.Zero(cowVA, proc.PageSize) // madvise(MADV_DONTNEED) on a page outside any area clears it
	if str(t, m, cowVA, 6) != "\x00\x00\x00\x00\x00\x00" || str(t, c, cowVA, 13) != "parent page 0" {
		t.Fatalf("parent %q, child %q", str(t, m, cowVA, 6), str(t, c, cowVA, 13))
	}
}

func TestCowReadOnlyPageStillFaults(t *testing.T) {
	nu := proctest.New()
	m, _ := proc.NewMem(nu)
	m.MapAnon(cowVA, proc.PageSize, 0) // read-only: reading is always allowed
	c, _ := m.Clone()
	if f := c.FaultIn(cowVA, true); f != proc.FaultSignal {
		t.Fatalf("write to a read-only page after fork = %v, want SIGSEGV", f)
	}
	if err := c.CopyOut(cowVA, []byte("x")); err == nil {
		t.Fatal("the kernel wrote a read-only page")
	}
}

// Through the dispatcher, as the nucleus reports it: a write to a present,
// read-only page (error code present|write|user = 7) after fork resumes,
// and only the writer's copy changes. A write to a page that is read-only
// for real is still SIGSEGV.
func TestCowWriteFaultThroughTheDispatcher(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	buf := uint64(r.call(p, 9, 0, 4096, 3, 0x22, ^uint64(0), 0))
	r.str(p, buf, "before fork")
	kid := r.call(p, 57)
	if kid <= 0 {
		t.Fatalf("fork = %d", kid)
	}
	shared := pa(t, r.nu, p.Mem(), buf)
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: proc.FaultBase | 14, Args: [6]uint64{buf, 0x400200, 7}})
	if v := r.await(p.TID()); v != 0 {
		t.Fatalf("write fault on a shared page: reply %d, want a resume", v)
	}
	if pa(t, r.nu, p.Mem(), buf) == shared || !writable(r.nu, p.Mem(), buf) {
		t.Error("the writer should have its own writable copy now")
	}
	ro := uint64(r.call(p, 9, 0, 4096, 1, 0x22, ^uint64(0), 0)) // PROT_READ
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: proc.FaultBase | 14, Args: [6]uint64{ro, 0x400200, 7}})
	if st, why := wait(t, p); st != 139 || why == "" {
		t.Errorf("write to a read-only page: status %d %q, want SIGSEGV", st, why)
	}
}
