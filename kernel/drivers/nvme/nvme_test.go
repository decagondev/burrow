package nvme

// nvme_test.go - the driver against a fake controller: left enabled as a
// firmware leaves it, reset, identified, its I/O queues created, then
// commands served from the submission queues in the driver's DMA pages when
// a doorbell rings, completions posted with the phase bit (so the queues
// wrap many times under blocktest), the interrupt raised. A command at a
// bad block fails with its status, and the disk goes on.

import (
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"burrow/kernel/block/blocktest"
	"burrow/kernel/virtio/virtiotest"
)

type fakeQueue struct {
	sq, cq     uint64
	entries    uint16
	head, tail uint16 // the submissions: the next to take, the driver's tail
	posted     uint16 // the next completion slot
	phase      uint16
}

type fakeCtrl struct {
	mu       sync.Mutex
	regs     map[uint32]uint32
	mem      virtiotest.Memory
	q        map[uint16]*fakeQueue
	image    []byte
	bad      int64
	flushes  atomic.Int32
	disk     atomic.Pointer[Disk]
	commands int
}

func newCtrl(blocks int) *fakeCtrl {
	f := &fakeCtrl{regs: map[uint32]uint32{}, q: map[uint16]*fakeQueue{}, image: blocktest.Pattern(blocks, 512), bad: -1}
	f.regs[regCAP] = 1023 | 1<<16 | 2<<24
	f.regs[regCC], f.regs[regCSTS] = ccEnable, cstsReady // the firmware's
	return f
}

func (f *fakeCtrl) R32(off uint32) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.regs[off]
}

func (f *fakeCtrl) W32(off uint32, v uint32) {
	f.mu.Lock()
	f.regs[off] = v
	switch {
	case off == regCC:
		f.regs[regCSTS] = v & ccEnable
		if v&ccEnable != 0 {
			aqa := f.regs[regAQA]
			f.q[0] = &fakeQueue{sq: uint64(f.regs[regASQ]) | uint64(f.regs[regASQ+4])<<32,
				cq: uint64(f.regs[regACQ]) | uint64(f.regs[regACQ+4])<<32, entries: uint16(aqa&0xFFF) + 1, phase: 1}
		}
	case off >= doorbell && (off-doorbell)%8 == 0: // a submission tail
		qid := uint16((off - doorbell) / 8)
		if q := f.q[qid]; q != nil {
			q.tail = uint16(v)
			go f.serve(qid) // a doorbell write returns at once; the controller works meanwhile
		}
	}
	f.mu.Unlock()
}

// serve runs every submission between the queue's head and tail.
func (f *fakeCtrl) serve(qid uint16) {
	f.mu.Lock()
	q := f.q[qid]
	for q.head != q.tail {
		e := f.mem.At(q.sq+uint64(q.head)*sqeBytes, sqeBytes)
		q.head = (q.head + 1) % q.entries
		status := f.run(qid, e)
		c := f.mem.At(q.cq+uint64(q.posted)*cqeBytes, cqeBytes)
		le.PutUint32(c[8:], uint32(q.head)|uint32(qid)<<16)
		dw3 := uint32(le.Uint16(e[2:])) | uint32(q.phase)<<16 | uint32(status)<<17
		atomic.StoreUint32((*uint32)(unsafe.Pointer(&c[12])), dw3) // the phase last, as a controller posts it
		q.advance()
	}
	f.mu.Unlock()
	if d := f.disk.Load(); d != nil && qid == ioQueue {
		d.Interrupt()
	}
}

func (q *fakeQueue) advance() {
	q.posted++
	if q.posted == q.entries {
		q.posted, q.phase = 0, q.phase^1
	}
}

// run executes one command (the lock held); its status.
func (f *fakeCtrl) run(qid uint16, e []byte) uint16 {
	op, prp1, prp2 := e[0], le.Uint64(e[24:]), le.Uint64(e[32:])
	cdw := func(i int) uint32 { return le.Uint32(e[40+4*i:]) }
	f.commands++
	if qid == 0 {
		switch op {
		case opIdentify:
			b := f.mem.At(prp1, pageBytes)
			if cdw(0) == cnsController {
				copy(b[24:], "QEMU NVMe Ctrl")
				b[77] = 3 // 8 pages at most
			} else {
				le.PutUint64(b[0:], uint64(len(f.image)/512))
				le.PutUint32(b[128:], 9<<16)
			}
		case opCreateCQ:
			f.q[uint16(cdw(0))] = &fakeQueue{cq: prp1, entries: uint16(cdw(0)>>16) + 1, phase: 1}
		case opCreateSQ:
			f.q[uint16(cdw(0))].sq = prp1
		}
		return 0
	}
	if op == opFlush {
		f.flushes.Add(1)
		return 0
	}
	lba, n := int64(cdw(0)), int(cdw(2))+1
	if f.bad >= lba && f.bad < lba+int64(n) {
		return 0x281 // an unrecovered read error, for instance
	}
	pages := []uint64{prp1}
	switch left := (n*512 + pageBytes - 1) / pageBytes; {
	case left == 2:
		pages = append(pages, prp2)
	case left > 2:
		list := f.mem.At(prp2, pageBytes)
		for k := 1; k < left; k++ {
			pages = append(pages, le.Uint64(list[8*(k-1):]))
		}
	}
	pos := int(lba) * 512
	for k, pa := range pages {
		size := min(pageBytes, n*512-k*pageBytes)
		if op == opRead {
			copy(f.mem.At(pa, size), f.image[pos:])
		} else {
			copy(f.image[pos:], f.mem.At(pa, size))
		}
		pos += size
	}
	return 0
}

func setUp(t *testing.T, blocks int) (*fakeCtrl, *Disk) {
	t.Helper()
	f := newCtrl(blocks)
	d, err := New(f, f.mem.Page)
	if err != nil {
		t.Fatal(err)
	}
	f.disk.Store(d)
	return f, d
}

func TestSetUp(t *testing.T) {
	f, d := setUp(t, 2048)
	if d.Sectors() != 2048 || d.SectorSize() != 512 || d.Model != "QEMU NVMe Ctrl" || d.pages != 8 {
		t.Errorf("%d blocks of %d, model %q, %d pages per command", d.Sectors(), d.SectorSize(), d.Model, d.pages)
	}
	if f.q[ioQueue] == nil || f.q[ioQueue].entries != ioEntries {
		t.Errorf("I/O queue %+v", f.q[ioQueue])
	}
}

func TestConformance(t *testing.T) {
	f, d := setUp(t, 2048) // whole-disk reads: 32 commands of 8 pages at once
	blocktest.Run(t, d, append([]byte(nil), f.image...))
	blocktest.RunWriter(t, d)
	if f.flushes.Load() == 0 {
		t.Error("RunWriter's flushes never reached the controller")
	}
	d.mu.Lock()
	n := d.Requests
	d.mu.Unlock()
	if n < ioEntries*2 {
		t.Errorf("only %d commands: the queues never wrapped twice", n)
	}
}

func TestCommandError(t *testing.T) {
	f, d := setUp(t, 256)
	f.mu.Lock()
	f.bad = 40
	f.mu.Unlock()
	if err := d.ReadSectors(32, make([]byte, 16*512)); err == nil {
		t.Error("a read over a bad block succeeded")
	}
	if err := d.ReadSectors(64, make([]byte, 512)); err != nil {
		t.Errorf("the disk after an error: %v", err)
	}
}
