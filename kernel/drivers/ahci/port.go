// port.go - one SATA port: started on our command list, its disk
// identified, then READ/WRITE DMA EXT and FLUSH CACHE EXT through command
// slot 0, one command at a time. The interrupt (HBA.Interrupt) ends a
// command; a command that is not answered in waitMax fails, and the port
// is not used again until it answers. Never touches data in the DMA pages
// while the HBA owns them.
package ahci

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"burrow/kernel/block"
)

const (
	waitMax  = 10 * time.Second
	stopWait = 500 * time.Millisecond
)

// DMAPage hands out one zeroed DMA page (driver.Env.DMAPage).
type DMAPage func() (pa uint64, mem []byte, err error)

// Port is a port with an ATA disk on it.
type Port struct {
	r        Regs
	base     uint32
	Number   int
	mu       sync.Mutex // one command at a time
	page     []byte
	pagePA   uint64
	data     [][]byte
	dataPA   []uint64
	done     chan error
	busy     bool // a command is in flight (under imu)
	imu      sync.Mutex
	sectors  uint64
	Model    string
	Requests uint64
}

// stop clears ST and FRE and waits until the HBA has let go of the lists.
func stop(r Regs, base uint32) error {
	r.W32(base+pxCMD, r.R32(base+pxCMD)&^cmdST)
	if !waitClear(r, base+pxCMD, cmdCR, stopWait) {
		return errors.New("the command list does not stop")
	}
	r.W32(base+pxCMD, r.R32(base+pxCMD)&^cmdFRE)
	if !waitClear(r, base+pxCMD, cmdFR, stopWait) {
		return errors.New("FIS receive does not stop")
	}
	return nil
}

func waitClear(r Regs, off, bits uint32, max time.Duration) bool {
	for deadline := time.Now().Add(max); r.R32(off)&bits != 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			return false
		}
	}
	return true
}

// newPort takes port n over: stopped, our lists, errors cleared, started.
// It returns nil, nil when no ATA disk is attached.
func newPort(r Regs, n int, dma DMAPage) (*Port, error) {
	base := uint32(portBase + n*portBytes)
	if r.R32(base+pxSSTS)&sstsDET != detUp || r.R32(base+pxSIG) != sigATA {
		return nil, nil
	}
	if err := stop(r, base); err != nil {
		return nil, err
	}
	p := &Port{r: r, base: base, Number: n, done: make(chan error, 1)}
	var err error
	if p.pagePA, p.page, err = dma(); err != nil {
		return nil, err
	}
	for k := 0; k < pagesMax; k++ {
		pa, mem, err := dma()
		if err != nil {
			return nil, err
		}
		p.data, p.dataPA = append(p.data, mem), append(p.dataPA, pa)
	}
	r.W32(base+pxCLB, uint32(p.pagePA+clbOff))
	r.W32(base+pxCLBU, uint32((p.pagePA+clbOff)>>32))
	r.W32(base+pxFB, uint32(p.pagePA+fbOff))
	r.W32(base+pxFBU, uint32((p.pagePA+fbOff)>>32))
	r.W32(base+pxSERR, 0xFFFFFFFF)
	r.W32(base+pxIS, 0xFFFFFFFF)
	r.W32(base+pxIE, ieAll)
	r.W32(base+pxCMD, r.R32(base+pxCMD)|cmdFRE)
	if !waitClear(r, base+pxTFD, tfdBSY|tfdDRQ, time.Second) {
		return nil, fmt.Errorf("port %d: the disk stays busy (tfd %#x)", n, r.R32(base+pxTFD))
	}
	r.W32(base+pxCMD, r.R32(base+pxCMD)|cmdST)
	return p, nil
}

// interrupt is the HBA's: it clears the port's causes and ends the command.
func (p *Port) interrupt() {
	is := p.r.R32(p.base + pxIS)
	p.r.W32(p.base+pxIS, is)
	p.imu.Lock()
	defer p.imu.Unlock()
	if !p.busy {
		return
	}
	var err error
	switch {
	case is&isTFES != 0:
		err = fmt.Errorf("port %d: task file error (tfd %#x)", p.Number, p.r.R32(p.base+pxTFD))
	case p.r.R32(p.base+pxCI)&1 != 0:
		return // not finished yet
	}
	p.busy = false
	p.done <- err
}

// run issues cmd in slot 0 with n bytes of data pages (written to the disk
// when write is set) and waits for its end. The caller holds p.mu.
func (p *Port) run(cmd byte, lba uint64, count uint16, n int, write bool) error {
	pages := (n + pageBytes - 1) / pageBytes
	flags := uint32(cflDwords) | uint32(pages)<<16
	if write {
		flags |= hdrWrite
	}
	hdr := p.page[clbOff:]
	le.PutUint32(hdr[0:], flags)
	le.PutUint32(hdr[4:], 0)
	ct := p.pagePA + ctOff
	le.PutUint32(hdr[8:], uint32(ct))
	le.PutUint32(hdr[12:], uint32(ct>>32))
	t := p.page[ctOff : ctOff+prdtOff+pagesMax*prdtBytes]
	clear(t)
	t[0], t[1], t[2] = fisRegH2D, fisCommand, cmd
	t[4], t[5], t[6] = byte(lba), byte(lba>>8), byte(lba>>16)
	t[8], t[9], t[10] = byte(lba>>24), byte(lba>>32), byte(lba>>40)
	if cmd != ataIdentify {
		t[7] = devLBA
	}
	t[12], t[13] = byte(count), byte(count>>8)
	for k := 0; k < pages; k++ {
		e := t[prdtOff+k*prdtBytes:]
		le.PutUint32(e[0:], uint32(p.dataPA[k]))
		le.PutUint32(e[4:], uint32(p.dataPA[k]>>32))
		le.PutUint32(e[12:], uint32(min(pageBytes, n-k*pageBytes)-1))
	}
	if !waitClear(p.r, p.base+pxTFD, tfdBSY|tfdDRQ, waitMax) {
		return fmt.Errorf("port %d: busy before the command (tfd %#x)", p.Number, p.r.R32(p.base+pxTFD))
	}
	p.imu.Lock()
	p.busy = true
	p.imu.Unlock()
	p.r.W32(p.base+pxCI, 1)
	select {
	case err := <-p.done:
		p.Requests++
		return err
	case <-time.After(waitMax):
		return fmt.Errorf("port %d: command %#x at sector %d not answered in %v", p.Number, cmd, lba, waitMax)
	}
}

// identify reads the disk's size and model.
func (p *Port) identify() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.run(ataIdentify, 0, 0, sectorBytes, false); err != nil {
		return err
	}
	w := func(i int) uint64 { return uint64(le.Uint16(p.data[0][2*i:])) }
	if w(identifyLBA4)&(1<<10) != 0 {
		p.sectors = w(100) | w(101)<<16 | w(102)<<32 | w(103)<<48
	} else {
		p.sectors = w(60) | w(61)<<16
	}
	var m []byte
	for i := 27; i <= 46; i++ { // two characters per word, the first in the high byte
		m = append(m, byte(w(i)>>8), byte(w(i)))
	}
	p.Model = strings.Trim(string(m), " \x00")
	if p.sectors == 0 {
		return errors.New("the disk reports no sectors")
	}
	return nil
}

func (p *Port) SectorSize() int { return sectorBytes }
func (p *Port) Sectors() uint64 { return p.sectors }

// ReadSectors reads buf from lba, pagesMax pages per command.
func (p *Port) ReadSectors(lba uint64, buf []byte) error {
	if err := block.CheckRange(p, lba, buf); err != nil {
		return err
	}
	return p.each(lba, buf, false)
}

// WriteSectors writes buf at lba.
func (p *Port) WriteSectors(lba uint64, buf []byte) error {
	if err := block.CheckRange(p, lba, buf); err != nil {
		return err
	}
	return p.each(lba, buf, true)
}

// Flush returns once the disk's write cache is on its medium.
func (p *Port) Flush() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.run(ataFlushExt, 0, 0, 0, false)
}

func (p *Port) each(lba uint64, buf []byte, write bool) error {
	chunk := pagesMax * pageBytes
	p.mu.Lock()
	defer p.mu.Unlock()
	for off := 0; off < len(buf); off += chunk {
		part := buf[off:min(len(buf), off+chunk)]
		if write {
			for k := 0; k*pageBytes < len(part); k++ {
				copy(p.data[k], part[k*pageBytes:])
			}
		}
		cmd := byte(ataReadExt)
		if write {
			cmd = ataWriteExt
		}
		at := lba + uint64(off/sectorBytes)
		if err := p.run(cmd, at, uint16(len(part)/sectorBytes), len(part), write); err != nil {
			return err
		}
		if !write {
			for k := 0; k*pageBytes < len(part); k++ {
				copy(part[k*pageBytes:min(len(part), (k+1)*pageBytes)], p.data[k])
			}
		}
	}
	return nil
}
