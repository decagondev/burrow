package e1000

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"burrow/kernel/netdev"
)

const (
	MTU          = 1500
	maxFrame     = MTU + 14 // the CRC is the card's
	resetWait    = 100 * time.Millisecond
	eepromWait   = 10 * time.Millisecond
	eepromMACLen = 3 // words 0-2 of the EEPROM
)

// NIC is a started card; it is a netdev.Device.
type NIC struct {
	regs Regs
	mac  netdev.MAC
	rx   *ring

	txMu sync.Mutex // Send may come from any goroutine
	tx   *ring

	mu    sync.Mutex
	recv  func([]byte)
	stats netdev.Stats
}

// New resets the card behind regs, reads its address, sets up both rings in
// pages from page and turns receiving, sending and interrupts on. The
// caller serves the interrupt line with Interrupt.
func New(regs Regs, page DMAPage) (*NIC, error) {
	n := &NIC{regs: regs}
	if err := n.reset(); err != nil {
		return nil, err
	}
	n.mac = n.readMAC()
	regs.Write(regRAL0, uint32(n.mac[0])|uint32(n.mac[1])<<8|uint32(n.mac[2])<<16|uint32(n.mac[3])<<24)
	regs.Write(regRAH0, uint32(n.mac[4])|uint32(n.mac[5])<<8|rahAV)
	for i := uint32(0); i < mtaWords; i++ {
		regs.Write(regMTA+4*i, 0)
	}
	var err error
	if n.rx, err = newRing(page); err != nil {
		return nil, err
	}
	if n.tx, err = newRing(page); err != nil {
		return nil, err
	}
	n.rx.armRX()
	n.startRings()
	regs.Write(regIMS, intWanted)
	return n, nil
}

func (n *NIC) reset() error {
	n.regs.Write(regIMC, ^uint32(0))
	n.regs.Write(regCTRL, n.regs.Read(regCTRL)|ctrlRST)
	for start := time.Now(); n.regs.Read(regCTRL)&ctrlRST != 0; time.Sleep(time.Millisecond) {
		if time.Since(start) > resetWait {
			return errors.New("the card did not come out of reset")
		}
	}
	n.regs.Write(regIMC, ^uint32(0))
	n.regs.Read(regICR)
	ctrl := n.regs.Read(regCTRL)
	n.regs.Write(regCTRL, (ctrl|ctrlSLU|ctrlASDE)&^(ctrlLRST|ctrlPHYRST|ctrlVME))
	return nil
}

// readMAC reads the address from the EEPROM (words 0-2); if the EEPROM does
// not answer, the address the card already holds in RAL0/RAH0.
func (n *NIC) readMAC() netdev.MAC {
	var mac netdev.MAC
	for w := uint32(0); w < eepromMACLen; w++ {
		n.regs.Write(regEERD, eerdStart|w<<8)
		start := time.Now()
		v := n.regs.Read(regEERD)
		for ; v&eerdDone == 0; v = n.regs.Read(regEERD) {
			if time.Since(start) > eepromWait {
				lo, hi := n.regs.Read(regRAL0), n.regs.Read(regRAH0)
				return netdev.MAC{byte(lo), byte(lo >> 8), byte(lo >> 16), byte(lo >> 24), byte(hi), byte(hi >> 8)}
			}
		}
		mac[2*w], mac[2*w+1] = byte(v>>16), byte(v>>24)
	}
	return mac
}

func (n *NIC) startRings() {
	r := n.regs
	r.Write(regRDBAL, uint32(n.rx.descPA))
	r.Write(regRDBAH, uint32(n.rx.descPA>>32))
	r.Write(regRDLEN, RingEntries*descBytes)
	r.Write(regRDH, 0)
	r.Write(regRDT, RingEntries-1) // every buffer but one is the card's
	r.Write(regRCTL, rctlEN|rctlBAM|rctlSECRC)
	r.Write(regTDBAL, uint32(n.tx.descPA))
	r.Write(regTDBAH, uint32(n.tx.descPA>>32))
	r.Write(regTDLEN, RingEntries*descBytes)
	r.Write(regTDH, 0)
	r.Write(regTDT, 0)
	r.Write(regTIPG, tipgIEEE)
	r.Write(regTCTL, tctlEN|tctlPSP|tctlCT|tctlCOLD)
}

// Interrupt drains the card after its line fired. On a shared line the
// cause register says whether it was this card at all.
func (n *NIC) Interrupt() {
	cause := n.regs.Read(regICR)
	if cause&(intRXT0|intRXO|intRXDMT0) != 0 {
		n.drainRX()
	}
	if cause&intTXDW != 0 {
		n.txMu.Lock()
		n.tx.reclaim()
		n.txMu.Unlock()
	}
}

func (n *NIC) drainRX() {
	for {
		frame, done := n.rx.received()
		if !done {
			return
		}
		n.regs.Write(regRDT, uint32(n.rx.next)) // hand the descriptor back
		n.rx.next = (n.rx.next + 1) % RingEntries
		n.mu.Lock()
		recv := n.recv
		if frame == nil || recv == nil {
			n.stats.RxDropped++
		} else {
			n.stats.RxFrames++
			n.stats.RxBytes += uint64(len(frame))
		}
		n.mu.Unlock()
		if frame != nil && recv != nil {
			recv(frame)
		}
	}
}

func (n *NIC) Send(frame []byte) error {
	if len(frame) > maxFrame {
		return fmt.Errorf("e1000: a %d-byte frame is over %d", len(frame), maxFrame)
	}
	n.txMu.Lock()
	ok := n.tx.queue(frame)
	if ok {
		n.regs.Write(regTDT, uint32(n.tx.next))
	}
	n.txMu.Unlock()
	n.mu.Lock()
	defer n.mu.Unlock()
	if !ok {
		n.stats.TxBusy++
		return netdev.ErrBusy
	}
	n.stats.TxFrames++
	n.stats.TxBytes += uint64(len(frame))
	return nil
}

func (n *NIC) SetReceiver(f func([]byte)) {
	n.mu.Lock()
	n.recv = f
	n.mu.Unlock()
}

func (n *NIC) Stats() netdev.Stats {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.stats
}

func (n *NIC) MAC() netdev.MAC { return n.mac }
func (n *NIC) MTU() int        { return MTU }
func (n *NIC) LinkUp() bool    { return n.regs.Read(regSTATUS)&statusLU != 0 }
