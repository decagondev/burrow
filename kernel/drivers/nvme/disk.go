// disk.go - namespace 1 as a block.Writer: each command takes a slot (its
// command identifier, data pages and PRP list page), is submitted on the
// I/O queue, and is ended by Interrupt; a long request is split into
// commands that run together.
package nvme

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"burrow/kernel/block"
)

const waitMax = 10 * time.Second

// Disk is a set-up controller's namespace 1.
type Disk struct {
	r          Regs
	dma        DMAPage
	stride     uint32
	admin, io  *queue
	nextAdmin  uint16
	mu         sync.Mutex // the I/O queue
	slots      []*slot
	free       chan *slot
	pages      int
	sectors    uint64
	blockBytes int
	Model      string
	Requests   uint64
}

type slot struct {
	cid    uint16
	data   [][]byte
	dataPA []uint64
	prp    []byte
	prpPA  uint64
	done   chan uint16
}

// New resets the controller and sets namespace 1 up for I/O.
func New(r Regs, dma DMAPage) (*Disk, error) {
	d := &Disk{r: r, dma: dma, pages: pagesMax}
	if err := d.reset(); err != nil {
		return nil, err
	}
	if err := d.identify(); err != nil {
		return nil, err
	}
	if err := d.createIOQueues(); err != nil {
		return nil, err
	}
	d.free = make(chan *slot, slotsMax)
	for i := 0; i < slotsMax; i++ {
		s := &slot{cid: uint16(i), done: make(chan uint16, 1)}
		var err error
		if s.prpPA, s.prp, err = dma(); err != nil {
			return nil, err
		}
		for k := 0; k < d.pages; k++ {
			pa, mem, err := dma()
			if err != nil {
				return nil, err
			}
			s.data, s.dataPA = append(s.data, mem), append(s.dataPA, pa)
		}
		d.slots = append(d.slots, s)
		d.free <- s
	}
	return d, nil
}

// Interrupt ends every command the I/O queue completed. With MSI-X the
// vector is the controller's own: it is always the caller's interrupt.
func (d *Disk) Interrupt() bool {
	d.mu.Lock()
	done := d.io.take()
	d.Requests += uint64(len(done))
	d.mu.Unlock()
	for _, c := range done {
		if int(c.cid) < len(d.slots) {
			d.slots[c.cid].done <- c.status
		}
	}
	return true
}

// do runs one command of at most d.pages pages on namespace 1.
func (d *Disk) do(op byte, lba uint64, buf []byte) error {
	s := <-d.free
	pages := (len(buf) + pageBytes - 1) / pageBytes
	c := command{op: op, cid: s.cid, nsid: namespace}
	if pages > 0 {
		c.prp1 = s.dataPA[0]
		c.cdw[0], c.cdw[1] = uint32(lba), uint32(lba>>32)
		c.cdw[2] = uint32(len(buf)/d.blockBytes - 1)
	}
	switch {
	case pages == 2:
		c.prp2 = s.dataPA[1]
	case pages > 2:
		for k := 1; k < pages; k++ {
			le.PutUint64(s.prp[8*(k-1):], s.dataPA[k])
		}
		c.prp2 = s.prpPA
	}
	if op == opWrite {
		for k := 0; k < pages; k++ {
			copy(s.data[k], buf[k*pageBytes:])
		}
	}
	d.mu.Lock()
	d.io.submit(c)
	d.mu.Unlock()
	var st uint16
	select {
	case st = <-s.done:
	case <-time.After(waitMax):
		return fmt.Errorf("nvme: command %#x at block %d not answered in %v", op, lba, waitMax) // the slot stays lost
	}
	defer func() { d.free <- s }()
	if st != 0 {
		return fmt.Errorf("nvme: command %#x at block %d: status %#x", op, lba, st)
	}
	if op == opRead {
		for k := 0; k < pages; k++ {
			copy(buf[k*pageBytes:min(len(buf), (k+1)*pageBytes)], s.data[k])
		}
	}
	return nil
}

func (d *Disk) SectorSize() int { return d.blockBytes }
func (d *Disk) Sectors() uint64 { return d.sectors }

// ReadSectors reads buf from lba.
func (d *Disk) ReadSectors(lba uint64, buf []byte) error {
	if err := block.CheckRange(d, lba, buf); err != nil {
		return err
	}
	return d.each(opRead, lba, buf)
}

// WriteSectors writes buf at lba.
func (d *Disk) WriteSectors(lba uint64, buf []byte) error {
	if err := block.CheckRange(d, lba, buf); err != nil {
		return err
	}
	return d.each(opWrite, lba, buf)
}

// Flush returns once every earlier write is on the medium.
func (d *Disk) Flush() error { return d.do(opFlush, 0, nil) }

// each splits buf into commands of d.pages pages and runs them together.
func (d *Disk) each(op byte, lba uint64, buf []byte) error {
	chunk := d.pages * pageBytes
	var wg sync.WaitGroup
	errs := make([]error, (len(buf)+chunk-1)/chunk)
	for i := range errs {
		part := buf[i*chunk : min(len(buf), (i+1)*chunk)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = d.do(op, lba+uint64(i*chunk/d.blockBytes), part)
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}
