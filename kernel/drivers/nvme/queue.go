// queue.go - a submission and completion queue pair in DMA pages: entries
// written at the tail, completions taken while their phase bit says they
// are new, both doorbells rung. The caller serializes each side. A
// completion's last dword is the controller's to write: it is read
// atomically, before the rest of the entry.
package nvme

import (
	"sync/atomic"
	"unsafe"
)

// queue is one pair; its completion queue fits one page.
type queue struct {
	r          Regs
	id         uint16
	entries    uint16
	stride     uint32 // doorbell stride in bytes
	sq, cq     []byte
	sqPA, cqPA uint64
	tail, head uint16
	phase      uint16 // the phase bit a new completion carries
}

func newQueue(r Regs, id, entries uint16, stride uint32, dma DMAPage) (*queue, error) {
	q := &queue{r: r, id: id, entries: entries, stride: stride, phase: 1}
	var err error
	if q.sqPA, q.sq, err = dma(); err != nil {
		return nil, err
	}
	if q.cqPA, q.cq, err = dma(); err != nil {
		return nil, err
	}
	return q, nil
}

// command is a submission entry's fields Burrow uses.
type command struct {
	op         byte
	cid        uint16
	nsid       uint32
	prp1, prp2 uint64
	cdw        [6]uint32 // CDW10-15
}

// submit writes c at the tail and rings the submission doorbell.
func (q *queue) submit(c command) {
	e := q.sq[int(q.tail)*sqeBytes:][:sqeBytes]
	clear(e)
	le.PutUint32(e[0:], uint32(c.op)|uint32(c.cid)<<16)
	le.PutUint32(e[4:], c.nsid)
	le.PutUint64(e[24:], c.prp1)
	le.PutUint64(e[32:], c.prp2)
	for i, v := range c.cdw {
		le.PutUint32(e[40+4*i:], v)
	}
	q.tail = (q.tail + 1) % q.entries
	q.r.W32(doorbell+uint32(2*q.id)*q.stride, uint32(q.tail))
}

// completion is a completion entry: which command, and its status (0 = success).
type completion struct {
	cid    uint16
	status uint16
}

// take returns the new completions and rings the completion doorbell.
func (q *queue) take() []completion {
	var out []completion
	for {
		e := q.cq[int(q.head)*cqeBytes:][:cqeBytes]
		dw3 := atomic.LoadUint32((*uint32)(unsafe.Pointer(&e[12])))
		if uint16(dw3>>16)&1 != q.phase {
			break
		}
		out = append(out, completion{cid: uint16(dw3), status: uint16(dw3 >> 17)})
		q.head++
		if q.head == q.entries {
			q.head, q.phase = 0, q.phase^1
		}
	}
	if len(out) > 0 {
		q.r.W32(doorbell+uint32(2*q.id+1)*q.stride, uint32(q.head))
	}
	return out
}
