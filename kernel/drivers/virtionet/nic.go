// Package virtionet drives virtio network cards (W23-S05): QEMU's
// virtio-net-pci as a netdev.Device. Queue 0 receives into 2 KiB DMA
// buffers, each offered again once its frame is handed on; queue 1 sends
// from slots of the same size, freed when the device returns them. Every
// frame carries the 12-byte virtio_net_hdr, zeroed: no checksum, TSO or
// merged buffers are negotiated (gVisor computes checksums). Never blocks:
// Send says ErrBusy when no slot is free. Docs: WAVE-23-proc-virtio.md.
package virtionet

import (
	"errors"
	"sync"

	"burrow/kernel/netdev"
	"burrow/kernel/virtio"
)

const (
	featMAC    = 1 << 5  // VIRTIO_NET_F_MAC
	featStatus = 1 << 16 // VIRTIO_NET_F_STATUS

	cfgMAC    = 0
	cfgStatus = 6 // u16, bit 0: link up

	hdrBytes  = 12 // struct virtio_net_hdr with num_buffers (virtio 1.0)
	bufBytes  = 2048
	pageBytes = 4096
	queueMax  = 128
	bufsMax   = 64
	mtu       = 1500
	ethMax    = mtu + 14
)

// DMAPage hands out one zeroed DMA page (driver.Env.DMAPage).
type DMAPage func() (pa uint64, mem []byte, err error)

type buf struct {
	pa  uint64
	mem []byte
}

// NIC is a set-up virtio network card.
type NIC struct {
	dev       *virtio.Device
	rx, tx    *virtio.Queue
	rxBufs    []buf
	txBufs    []buf
	mu        sync.Mutex // tx's queue and free slots; rx is the interrupt's alone
	txFree    []uint16
	mac       netdev.MAC
	hasStatus bool
	recv      func([]byte)
	recvMu    sync.Mutex
	stats     netdev.Stats
	statsMu   sync.Mutex
}

// New sets the card up: features, its address, both queues, every receive buffer offered.
func New(dev *virtio.Device, dma DMAPage) (*NIC, error) {
	agreed, err := dev.Negotiate(featMAC | featStatus)
	if err != nil {
		return nil, err
	}
	n := &NIC{dev: dev, hasStatus: agreed&featStatus != 0}
	if agreed&featMAC == 0 {
		return nil, errors.New("virtionet: the card has no MAC address to give")
	}
	for i := range n.mac {
		n.mac[i] = dev.Config.R8(cfgMAC + uint32(i))
	}
	if n.rx, n.rxBufs, err = setup(dev, dma, 0); err != nil {
		return nil, err
	}
	if n.tx, n.txBufs, err = setup(dev, dma, 1); err != nil {
		return nil, err
	}
	for i := range n.rxBufs {
		n.rx.SetDesc(uint16(i), n.rxBufs[i].pa, bufBytes, virtio.DescWrite)
		n.rx.Offer(uint16(i))
	}
	for i := range n.txBufs {
		n.txFree = append(n.txFree, uint16(i))
	}
	dev.Ready()
	dev.Kick(n.rx)
	return n, nil
}

// setup makes queue idx (its rings in one page) and its buffers.
func setup(dev *virtio.Device, dma DMAPage, idx uint16) (*virtio.Queue, []buf, error) {
	size := min(dev.QueueMax(idx), queueMax)
	if size == 0 {
		return nil, nil, errors.New("virtionet: a queue is missing")
	}
	pa, mem, err := dma()
	if err != nil {
		return nil, nil, err
	}
	q, err := virtio.NewQueue(idx, size, pa, mem)
	if err != nil {
		return nil, nil, err
	}
	var bufs []buf
	for len(bufs) < min(int(size), bufsMax) {
		pa, mem, err := dma()
		if err != nil {
			return nil, nil, err
		}
		for off := 0; off+bufBytes <= pageBytes; off += bufBytes {
			bufs = append(bufs, buf{pa + uint64(off), mem[off : off+bufBytes]})
		}
	}
	return q, bufs, dev.SetupQueue(q)
}

func (n *NIC) MAC() netdev.MAC { return n.mac }

// Driver names the driver in the kernel's log (the boot's ARP check).
func (n *NIC) Driver() string { return "virtionet" }
func (n *NIC) MTU() int       { return mtu }

// LinkUp is the device's link status (up when it does not report one).
func (n *NIC) LinkUp() bool {
	return !n.hasStatus || n.dev.Config.R16(cfgStatus)&1 != 0
}

func (n *NIC) SetReceiver(f func([]byte)) {
	n.recvMu.Lock()
	n.recv = f
	n.recvMu.Unlock()
}

func (n *NIC) Stats() netdev.Stats {
	n.statsMu.Lock()
	defer n.statsMu.Unlock()
	return n.stats
}

func (n *NIC) count(f func(s *netdev.Stats)) {
	n.statsMu.Lock()
	f(&n.stats)
	n.statsMu.Unlock()
}

// Send queues one frame behind a zeroed header.
func (n *NIC) Send(frame []byte) error {
	if len(frame) > ethMax {
		return errors.New("virtionet: frame longer than the MTU")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.txFree) == 0 {
		n.count(func(s *netdev.Stats) { s.TxBusy++ })
		return netdev.ErrBusy
	}
	i := n.txFree[len(n.txFree)-1]
	n.txFree = n.txFree[:len(n.txFree)-1]
	b := n.txBufs[i]
	clear(b.mem[:hdrBytes])
	copy(b.mem[hdrBytes:], frame)
	n.tx.SetDesc(i, b.pa, uint32(hdrBytes+len(frame)), 0)
	n.tx.Offer(i)
	n.dev.Kick(n.tx)
	n.count(func(s *netdev.Stats) { s.TxFrames++; s.TxBytes += uint64(len(frame)) })
	return nil
}

// Interrupt takes back sent slots and received frames; false: not its interrupt.
func (n *NIC) Interrupt() bool {
	if n.dev.ReadISR() == 0 {
		return false
	}
	n.mu.Lock()
	for {
		i, _, ok := n.tx.Used()
		if !ok {
			break
		}
		n.txFree = append(n.txFree, i)
	}
	n.mu.Unlock()
	n.receive()
	return true
}

// receive hands on every frame the device filled and offers its buffer again.
func (n *NIC) receive() {
	n.recvMu.Lock()
	recv := n.recv
	n.recvMu.Unlock()
	offered := false
	for {
		i, length, ok := n.rx.Used()
		if !ok {
			break
		}
		if int(i) < len(n.rxBufs) && length > hdrBytes && length <= bufBytes {
			frame := append([]byte(nil), n.rxBufs[i].mem[hdrBytes:length]...)
			n.count(func(s *netdev.Stats) { s.RxFrames++; s.RxBytes += uint64(len(frame)) })
			if recv != nil {
				recv(frame)
			} else {
				n.count(func(s *netdev.Stats) { s.RxDropped++ })
			}
		}
		if int(i) < len(n.rxBufs) {
			n.rx.Offer(i)
			offered = true
		}
	}
	if offered {
		n.dev.Kick(n.rx)
	}
}
