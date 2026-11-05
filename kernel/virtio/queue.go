// queue.go - a split virtqueue (§2.4) in one DMA page: the descriptor table,
// then the available ring (the driver's), then the used ring (the device's),
// each aligned as the spec asks. The rings' indices are 16-bit fields next
// to 16-bit flags, read and written as one aligned 32-bit word with atomics,
// so the index is published after the entries it covers and read before
// the entries it covers (x86 keeps stores in order; the atomics keep the
// compiler from moving them). Never touches a descriptor the device owns.
package virtio

import (
	"encoding/binary"
	"fmt"
	"sync/atomic"
	"unsafe"
)

const (
	descBytes  = 16
	DescWrite  = 2 // VIRTQ_DESC_F_WRITE: the device writes this buffer
	ringHeader = 4 // flags u16, idx u16
	usedElem   = 8 // id u32, len u32
	maxSize    = 256
)

var le = binary.LittleEndian

// Queue is one split virtqueue.
type Queue struct {
	Index, Size        uint16
	mem                []byte
	pa                 uint64
	availOff, usedOff  int
	availIdx, lastUsed uint16
	notifyOff          uint32
}

// NewQueue lays out queue idx of size entries (a power of two) in mem,
// which the device reaches at physical pa.
func NewQueue(idx, size uint16, pa uint64, mem []byte) (*Queue, error) {
	if size == 0 || size > maxSize || size&(size-1) != 0 {
		return nil, fmt.Errorf("virtio: queue size %d", size)
	}
	q := &Queue{Index: idx, Size: size, mem: mem, pa: pa}
	q.availOff = int(size) * descBytes
	q.usedOff = (q.availOff + ringHeader + 2*int(size) + 2 + 3) &^ 3
	if end := q.usedOff + ringHeader + usedElem*int(size) + 2; end > len(mem) {
		return nil, fmt.Errorf("virtio: queue of %d needs %d bytes, the page has %d", size, end, len(mem))
	}
	clear(mem)
	return q, nil
}

func (q *Queue) DescPA() uint64  { return q.pa }
func (q *Queue) AvailPA() uint64 { return q.pa + uint64(q.availOff) }
func (q *Queue) UsedPA() uint64  { return q.pa + uint64(q.usedOff) }

// SetDesc fills descriptor i: length bytes at addr, flags (DescWrite).
func (q *Queue) SetDesc(i uint16, addr uint64, length uint32, flags uint16) {
	d := q.mem[int(i)*descBytes:]
	le.PutUint64(d, addr)
	le.PutUint32(d[8:], length)
	le.PutUint16(d[12:], flags)
	le.PutUint16(d[14:], 0)
}

func (q *Queue) word(off int) *uint32 { return (*uint32)(unsafe.Pointer(&q.mem[off])) }

// Offer puts descriptor chain head on the available ring and publishes it;
// the device sees it after Device.Kick.
func (q *Queue) Offer(head uint16) {
	slot := q.availOff + ringHeader + 2*int(q.availIdx%q.Size)
	le.PutUint16(q.mem[slot:], head)
	q.availIdx++
	atomic.StoreUint32(q.word(q.availOff), uint32(q.availIdx)<<16) // flags 0: interrupt us
}

// Used takes the next buffer the device returned: its descriptor and how
// many bytes it wrote; ok is false when there is none.
func (q *Queue) Used() (id uint16, length uint32, ok bool) {
	idx := uint16(atomic.LoadUint32(q.word(q.usedOff)) >> 16)
	if idx == q.lastUsed {
		return 0, 0, false
	}
	e := q.mem[q.usedOff+ringHeader+usedElem*int(q.lastUsed%q.Size):]
	q.lastUsed++
	return uint16(le.Uint32(e)), le.Uint32(e[4:]), true
}
