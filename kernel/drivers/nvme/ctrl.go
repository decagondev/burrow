// ctrl.go - the controller set up: disabled, the admin queue given, enabled,
// identified (the controller's model and largest transfer, namespace 1's
// size and block size), and the I/O queue pair created. Admin commands are
// polled: they run once, before the interrupt matters.
package nvme

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// DMAPage hands out one zeroed DMA page (driver.Env.DMAPage).
type DMAPage func() (pa uint64, mem []byte, err error)

const adminWait = 5 * time.Second

func r64(r Regs, off uint32) uint64 { return uint64(r.R32(off)) | uint64(r.R32(off+4))<<32 }
func w64(r Regs, off uint32, v uint64) {
	r.W32(off, uint32(v))
	r.W32(off+4, uint32(v>>32))
}

// waitReady waits for CSTS.RDY to be want, up to the controller's own timeout.
func waitReady(r Regs, want uint32, timeout time.Duration) error {
	for deadline := time.Now().Add(timeout); ; time.Sleep(time.Millisecond) {
		st := r.R32(regCSTS)
		if st&cstsFatal != 0 {
			return errors.New("the controller reports a fatal status")
		}
		if st&cstsReady == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("CSTS.RDY not %d after %v", want, timeout)
		}
	}
}

// reset disables the controller, gives it the admin queue and enables it.
func (d *Disk) reset() error {
	caps := r64(d.r, regCAP)
	timeout := time.Duration((caps>>24)&0xFF+1) * 500 * time.Millisecond
	d.stride = 4 << ((caps >> 32) & 0xF)
	if mqes := uint32(uint16(caps)) + 1; mqes < ioEntries {
		return fmt.Errorf("queues of at most %d entries", mqes)
	}
	if cc := d.r.R32(regCC); cc&ccEnable != 0 {
		d.r.W32(regCC, cc&^ccEnable)
	}
	if err := waitReady(d.r, 0, timeout); err != nil {
		return err
	}
	var err error
	if d.admin, err = newQueue(d.r, adminQueue, adminEntries, d.stride, d.dma); err != nil {
		return err
	}
	d.r.W32(regAQA, (adminEntries-1)<<16|(adminEntries-1))
	w64(d.r, regASQ, d.admin.sqPA)
	w64(d.r, regACQ, d.admin.cqPA)
	d.r.W32(regCC, ccEnable|ccIOSQES|ccIOCQES) // NVM command set, 4 KiB pages
	return waitReady(d.r, cstsReady, timeout)
}

// adminCmd runs c on the admin queue and polls for its completion.
func (d *Disk) adminCmd(c command) error {
	d.nextAdmin++
	c.cid = d.nextAdmin
	d.admin.submit(c)
	for deadline := time.Now().Add(adminWait); ; time.Sleep(100 * time.Microsecond) {
		for _, done := range d.admin.take() {
			if done.cid == c.cid {
				if done.status != 0 {
					return fmt.Errorf("admin command %#x: status %#x", c.op, done.status)
				}
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("admin command %#x not answered in %v", c.op, adminWait)
		}
	}
}

// identify reads the controller's model and transfer limit and namespace
// 1's size and block size.
func (d *Disk) identify() error {
	pa, buf, err := d.dma()
	if err != nil {
		return err
	}
	if err := d.adminCmd(command{op: opIdentify, prp1: pa, cdw: [6]uint32{cnsController}}); err != nil {
		return err
	}
	d.Model = strings.Trim(string(buf[24:64]), " \x00")
	if mdts := buf[77]; mdts != 0 && 1<<mdts < d.pages {
		d.pages = 1 << mdts
	}
	clear(buf)
	if err := d.adminCmd(command{op: opIdentify, nsid: namespace, prp1: pa, cdw: [6]uint32{cnsNamespace}}); err != nil {
		return err
	}
	d.sectors = le.Uint64(buf[0:])
	lbads := (le.Uint32(buf[128+4*int(buf[26]&0xF):]) >> 16) & 0xFF
	if lbads < 9 || lbads > 12 || d.sectors == 0 {
		return fmt.Errorf("namespace 1: %d blocks of 2^%d bytes", d.sectors, lbads)
	}
	d.blockBytes = 1 << lbads
	return nil
}

// createIOQueues makes the I/O pair, its completions on interrupt vector 0.
func (d *Disk) createIOQueues() error {
	var err error
	if d.io, err = newQueue(d.r, ioQueue, ioEntries, d.stride, d.dma); err != nil {
		return err
	}
	size := uint32(ioEntries-1)<<16 | ioQueue
	if err := d.adminCmd(command{op: opCreateCQ, prp1: d.io.cqPA,
		cdw: [6]uint32{size, cqIntEnabled | qPhysContig}}); err != nil {
		return err
	}
	return d.adminCmd(command{op: opCreateSQ, prp1: d.io.sqPA,
		cdw: [6]uint32{size, ioQueue<<16 | qPhysContig}})
}
