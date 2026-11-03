package proc

// devices.go - what a file may offer beyond read and write (W11-S01): its
// own ioctls (Ioctler), physical memory for mmap(MAP_SHARED) (Mappable),
// and readiness (Poller, poll.go), which also gives it O_NONBLOCK. A device
// file from devfs is unwrapped to its device first. Device memory is the
// device's: proc maps it but never frees or copies it (mem.go).
// Docs: docs/04-plan/waves/WAVE-11-doom.md.

import (
	"syscall"

	"burrow/kernel/fs/devfs"
)

const oNonblock = 0x800

// Ioctler is a file with ioctls of its own; in and out copy the caller's
// memory at an address (EFAULT on a bad one).
type Ioctler interface {
	Ioctl(req, arg uint64, in, out func(va uint64, b []byte) error) (int64, error)
}

// Mappable is device memory that mmap(MAP_SHARED) maps: size bytes from
// physical address pa (page-aligned).
type Mappable interface {
	PhysRange() (pa, size uint64)
}

// device is what f's interfaces are looked up on: the device behind a
// devfs file, else the file itself.
func device(f *file) any {
	if d, ok := f.f.(interface{ Device() devfs.Device }); ok {
		return d.Device()
	}
	return f.f
}

// wouldBlock: f is non-blocking and not ready for want (pollIn/pollOut).
func wouldBlock(f *file, want int16) bool {
	if f.flags&oNonblock == 0 {
		return false
	}
	pl, ok := device(f).(Poller)
	if !ok {
		return false
	}
	rev, _ := pl.Poll()
	return rev&(want|pollHup|pollErr) == 0
}

func (p *Process) deviceIoctl(d Ioctler, req, arg uint64) int64 {
	r, err := d.Ioctl(req, arg, p.mem.CopyIn, p.mem.CopyOut)
	if err != nil {
		return errno(err)
	}
	return r
}

// mmapDevice maps n bytes of fd's device memory from offset off at addr.
func (p *Process) mmapDevice(fd, off, n uint64, flags uint64, pr Prot, fixed bool, addr uint64) int64 {
	f := p.fds.get(fd)
	if f == nil {
		return -int64(syscall.EBADF)
	}
	m, ok := device(f).(Mappable)
	switch {
	case !ok:
		return -int64(syscall.ENODEV)
	case flags&mapShared == 0:
		return -int64(syscall.EINVAL) // a private copy of device memory: not supported
	}
	pa, size := m.PhysRange()
	if off%PageSize != 0 || off > size || n > pageUp(size)-off {
		return -int64(syscall.EINVAL)
	}
	addr, err := p.pickAddr(addr, n, fixed)
	if err != 0 {
		return err
	}
	if err := p.mem.MapDevice(addr, pa+off, n, pr); err != nil {
		return -int64(syscall.ENOMEM)
	}
	return int64(addr)
}
