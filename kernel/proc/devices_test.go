package proc_test

import (
	"io"
	"syscall"
	"testing"
	"time"

	"burrow/kernel/fs/devfs"
	"burrow/kernel/fs/memfs"
	"burrow/kernel/klog"
	"burrow/kernel/proc"
	"burrow/kernel/proc/proctest"
	"burrow/kernel/vfs"
)

const fbPhys, fbSize = 0xFD000000, 3 * proc.PageSize

// fakeFB is a device with an ioctl, memory to map and readiness.
type fakeFB struct {
	ready   bool
	changed chan struct{}
}

func (*fakeFB) Read([]byte) (int, error)     { return 0, io.EOF }
func (*fakeFB) Write(b []byte) (int, error)  { return len(b), nil }
func (*fakeFB) PhysRange() (pa, size uint64) { return fbPhys, fbSize }
func (d *fakeFB) Poll() (int16, <-chan struct{}) {
	if d.ready {
		return 0x1, d.changed
	}
	return 0x4, d.changed
}
func (*fakeFB) Ioctl(req, arg uint64, in, out func(uint64, []byte) error) (int64, error) {
	if req != 0x4600 {
		return 0, syscall.ENOTTY
	}
	return 7, out(arg, []byte{1, 2, 3, 4})
}

func devRig(t *testing.T, d devfs.Device) *rig {
	root := memfs.New()
	root.MkdirAll("bin", 0o755, time.Time{})
	root.WriteFile("bin/prog", program(), 0o755, time.Time{})
	root.Freeze()
	dev := devfs.New()
	dev.Register("fb0", d, 0o660)
	v := vfs.New()
	v.Mount(".", root, "memfs")
	v.Mount("dev", dev, "devfs")
	nu := proctest.New()
	r := &rig{t: t, nu: nu, console: &syncBuf{}, log: &syncBuf{}}
	r.sys = proc.NewSystem(nu, v, r.console, klog.New(r.log, klog.Info))
	go r.sys.Serve()
	return r
}

func TestDeviceIoctl(t *testing.T) {
	r := devRig(t, &fakeFB{})
	p := r.spawn()
	fd := r.call(p, 2, r.str(p, scratch, "/dev/fb0"), 2)
	if res := r.call(p, 16, uint64(fd), 0x4600, scratch+64); res != 7 {
		t.Fatalf("device ioctl = %d", res)
	}
	b := make([]byte, 4)
	p.Mem().CopyIn(scratch+64, b)
	if b[0] != 1 || b[3] != 4 {
		t.Errorf("ioctl wrote % x", b)
	}
	if e := r.call(p, 16, uint64(fd), 0x5401, scratch); e != -int64(syscall.ENOTTY) {
		t.Errorf("unknown device ioctl = %d", e)
	}
	if e := r.call(p, 16, uint64(fd), 0x4600, 0x10); e != -int64(syscall.EFAULT) {
		t.Errorf("ioctl to a bad address = %d", e)
	}
}

// Device memory is mapped where asked, shared by fork, never freed.
func TestMmapDeviceMemory(t *testing.T) {
	r := devRig(t, &fakeFB{})
	p := r.spawn()
	fd := uint64(r.call(p, 2, r.str(p, scratch, "/dev/fb0"), 2))
	if e := r.call(p, 9, 0, fbSize, 3, 0x02, fd, 0); e != -int64(syscall.EINVAL) {
		t.Errorf("MAP_PRIVATE of device memory = %d, want EINVAL", e)
	}
	if e := r.call(p, 9, 0, fbSize+proc.PageSize, 3, 0x01, fd, 0); e != -int64(syscall.EINVAL) {
		t.Errorf("mapping past the device = %d, want EINVAL", e)
	}
	before := r.nu.FramesInUse()
	va := uint64(r.call(p, 9, 0, fbSize, 3, 0x01, fd, 0))
	if pa, ok := r.nu.PA(p.Mem().Space(), va+proc.PageSize); !ok || pa != fbPhys+proc.PageSize {
		t.Fatalf("mmap at %#x: page 1 maps %#x (%v)", va, pa, ok)
	}
	if r.nu.FramesInUse() != before {
		t.Error("mapping device memory allocated frames")
	}
	if e := r.call(p, 1, 1, va, 4); e != -int64(syscall.EFAULT) {
		t.Errorf("write(2) from device memory = %d, want EFAULT (not reachable through the physmap)", e)
	}
	r.call(p, 57) // fork: the child shares the device pages
	kid := r.newestThread(p.TID())
	if pa, ok := r.nu.PA(r.nu.Snapshot(kid).AS, va); !ok || pa != fbPhys {
		t.Errorf("child maps %#x (%v), want the device page", pa, ok)
	}
	if r.call(p, 11, va, proc.PageSize) != 0 { // munmap must not free a device page (the fake panics on that)
		t.Error("munmap")
	}
	r.nu.Inject(proc.Request{TID: kid, Nr: 231})
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 231})
	wait(t, p)
	r.waitFor(func() bool { return r.nu.FramesInUse() == 0 })
}

func TestNonblockingRead(t *testing.T) {
	d := &fakeFB{changed: make(chan struct{})}
	r := devRig(t, d)
	p := r.spawn()
	fd := uint64(r.call(p, 2, r.str(p, scratch, "/dev/fb0"), 0x800)) // O_RDONLY|O_NONBLOCK
	if e := r.call(p, 0, fd, scratch+64, 16); e != -int64(syscall.EAGAIN) {
		t.Errorf("read of a device with nothing ready = %d, want EAGAIN", e)
	}
	d.ready = true
	if n := r.call(p, 0, fd, scratch+64, 16); n != 0 {
		t.Errorf("read once ready = %d", n)
	}
	r.call(p, 293, scratch, 0x800) // pipe2(O_NONBLOCK)
	b := make([]byte, 8)
	p.Mem().CopyIn(scratch, b)
	if e := r.call(p, 0, uint64(le.Uint32(b)), scratch+64, 1); e != -int64(syscall.EAGAIN) {
		t.Errorf("read of an empty non-blocking pipe = %d, want EAGAIN", e)
	}
}
