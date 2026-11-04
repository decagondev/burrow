package e1000

// Descriptor rings (8254x manual 3.2, 3.3): the card and the driver share
// an array of 16-byte descriptors, each pointing at one 2 KiB buffer. The
// card owns the descriptors from its head (RDH/TDH) up to the tail
// (RDT/TDT); the driver owns the rest and moves the tail to hand them over.
// Ring and buffers are pages from DMAPage: physical, never moved.

import (
	"encoding/binary"
	"fmt"
	"sync/atomic"
	"unsafe"
)

const (
	PageBytes   = 4096
	descBytes   = 16
	bufBytes    = 2048 // RCTL.BSIZE 00; two buffers to a page
	RingEntries = 64   // a ring is 1 KiB (a multiple of 128 bytes, as RDLEN wants)

	descStatus = 12 // byte offset of status in both descriptor kinds
	statusDD   = 1 << 0
	rxEOP      = 1 << 1
	txCmdEOP   = 1 << 0
	txCmdIFCS  = 1 << 1 // insert the CRC
	txCmdRS    = 1 << 3 // report status (set DD when done)
)

// DMAPage hands out one zeroed page the card can reach: its physical
// address and its bytes.
type DMAPage func() (pa uint64, mem []byte, err error)

type ring struct {
	desc   []byte // RingEntries descriptors
	descPA uint64
	buf    [RingEntries][]byte
	bufPA  [RingEntries]uint64
	next   int // receive: the next to read; transmit: the next free (the tail)
	clean  int // transmit: the oldest not yet seen done
}

func newRing(page DMAPage) (*ring, error) {
	r := &ring{}
	var err error
	if r.descPA, r.desc, err = page(); err != nil {
		return nil, fmt.Errorf("descriptor ring: %w", err)
	}
	r.desc = r.desc[:RingEntries*descBytes]
	for i := 0; i < RingEntries; i += PageBytes / bufBytes {
		pa, mem, err := page()
		if err != nil {
			return nil, fmt.Errorf("buffers: %w", err)
		}
		for j := 0; j < PageBytes/bufBytes; j++ {
			r.buf[i+j] = mem[j*bufBytes : (j+1)*bufBytes]
			r.bufPA[i+j] = pa + uint64(j*bufBytes)
		}
	}
	return r, nil
}

func (r *ring) d(i int) []byte { return r.desc[i*descBytes : (i+1)*descBytes] }

// status reads descriptor i's status byte with one load the card's write
// cannot tear and the compiler cannot cache.
func (r *ring) status(i int) uint8 {
	return uint8(atomic.LoadUint32((*uint32)(unsafe.Pointer(&r.d(i)[descStatus]))))
}

// armRX gives every descriptor its buffer for receiving.
func (r *ring) armRX() {
	for i := 0; i < RingEntries; i++ {
		d := r.d(i)
		clear(d)
		binary.LittleEndian.PutUint64(d[0:], r.bufPA[i])
	}
}

// received returns the frame in descriptor next if the card is done with
// it (whole, without errors: frames longer than a buffer are dropped), and
// whether there was one at all.
func (r *ring) received() (frame []byte, done bool) {
	d := r.d(r.next)
	st := r.status(r.next)
	if st&statusDD == 0 {
		return nil, false
	}
	n := int(binary.LittleEndian.Uint16(d[8:]))
	if st&rxEOP != 0 && d[13] == 0 && n <= bufBytes {
		frame = append([]byte(nil), r.buf[r.next][:n]...)
	}
	d[descStatus], d[13] = 0, 0
	return frame, true
}

// queue puts frame into the next free transmit descriptor, or reports that
// the ring is full (one slot always stays empty, so head == tail means idle).
func (r *ring) queue(frame []byte) bool {
	r.reclaim()
	if (r.next+1)%RingEntries == r.clean {
		return false
	}
	copy(r.buf[r.next], frame)
	d := r.d(r.next)
	binary.LittleEndian.PutUint64(d[0:], r.bufPA[r.next])
	binary.LittleEndian.PutUint16(d[8:], uint16(len(frame)))
	d[10], d[11] = 0, txCmdEOP|txCmdIFCS|txCmdRS
	d[descStatus], d[13] = 0, 0
	r.next = (r.next + 1) % RingEntries
	return true
}

// reclaim moves clean past every descriptor the card has sent.
func (r *ring) reclaim() {
	for r.clean != r.next && r.status(r.clean)&statusDD != 0 {
		r.clean = (r.clean + 1) % RingEntries
	}
}
