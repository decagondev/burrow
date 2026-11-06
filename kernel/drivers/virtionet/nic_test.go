package virtionet

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"burrow/kernel/netdev"
	"burrow/kernel/virtio/virtiotest"
)

// fakeCard is a virtio network card: what the driver sends is kept, and
// put delivers a frame into the next receive buffer offered.
type fakeCard struct {
	*virtiotest.Device
	mu     sync.Mutex
	sent   [][]byte
	rxPool []uint16
	rxBufs map[uint16]virtiotest.Desc
	nic    *NIC
}

func newFakeCard(t *testing.T, features uint64) *fakeCard {
	t.Helper()
	f := &fakeCard{Device: virtiotest.New(features, 128), rxBufs: map[uint16]virtiotest.Desc{}}
	copy(f.Config.B[cfgMAC:], []byte{0x52, 0x54, 0, 0x12, 0x34, 0x56})
	f.Config.B[cfgStatus] = 1
	f.OnKick = func(q uint16) {
		if q == 1 {
			f.transmit()
		}
	}
	nic, err := New(f.Virtio(), f.Mem.Page)
	if err != nil {
		t.Fatal(err)
	}
	f.nic = nic
	return f
}

// transmit takes what the driver offered on queue 1, keeps the frames and
// returns the slots.
func (f *fakeCard) transmit() {
	f.mu.Lock()
	defer f.mu.Unlock()
	heads, chains := f.Take(1)
	for i, c := range chains {
		b := f.Mem.At(c[0].Addr, int(c[0].Len))
		if !bytes.Equal(b[:hdrBytes], make([]byte, hdrBytes)) {
			panic("virtio_net_hdr not zeroed")
		}
		f.sent = append(f.sent, append([]byte(nil), b[hdrBytes:]...))
		f.Use(1, heads[i], 0)
	}
}

// put delivers frame into a receive buffer, as the wire would.
func (f *fakeCard) put(frame []byte) bool {
	f.mu.Lock()
	heads, chains := f.Take(0)
	for i, c := range chains {
		f.rxPool = append(f.rxPool, heads[i])
		f.rxBufs[heads[i]] = c[0]
	}
	if len(f.rxPool) == 0 {
		f.mu.Unlock()
		return false
	}
	h := f.rxPool[0]
	f.rxPool = f.rxPool[1:]
	d := f.rxBufs[h]
	b := f.Mem.At(d.Addr, int(d.Len))
	clear(b[:hdrBytes])
	copy(b[hdrBytes:], frame)
	f.Use(0, h, uint32(hdrBytes+len(frame)))
	f.mu.Unlock()
	f.nic.Interrupt()
	return true
}

func frame(n int, fill byte) []byte { return bytes.Repeat([]byte{fill}, n) }

func TestSendAndReceive(t *testing.T) {
	f := newFakeCard(t, featMAC|featStatus)
	if f.nic.MAC() != (netdev.MAC{0x52, 0x54, 0, 0x12, 0x34, 0x56}) || !f.nic.LinkUp() || f.nic.MTU() != 1500 {
		t.Fatalf("mac %v link %v", f.nic.MAC(), f.nic.LinkUp())
	}
	var got [][]byte
	var mu sync.Mutex
	f.nic.SetReceiver(func(b []byte) { mu.Lock(); got = append(got, b); mu.Unlock() })
	for i := 0; i < 300; i++ { // more than the buffers: each is offered again
		if !f.put(frame(60+i, byte(i))) {
			t.Fatalf("frame %d: no receive buffer offered", i)
		}
	}
	mu.Lock()
	if len(got) != 300 || !bytes.Equal(got[299], frame(359, byte(299%256))) {
		t.Fatalf("received %d frames", len(got))
	}
	mu.Unlock()
	if err := f.nic.Send(frame(1514, 7)); err != nil {
		t.Fatal(err)
	}
	if err := f.nic.Send(frame(1515, 7)); err == nil {
		t.Error("a frame longer than the MTU was sent")
	}
	st := f.nic.Stats()
	if st.RxFrames != 300 || st.TxFrames != 1 {
		t.Errorf("stats %+v", st)
	}
}

func TestSendWhenFullThenFreed(t *testing.T) {
	f := newFakeCard(t, featMAC)
	f.OnKick = nil // the device sends nothing: the slots fill
	n := 0
	for ; n < 1000; n++ {
		if err := f.nic.Send(frame(64, 1)); errors.Is(err, netdev.ErrBusy) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if n != bufsMax || f.nic.Stats().TxBusy != 1 {
		t.Fatalf("%d sent before busy, stats %+v", n, f.nic.Stats())
	}
	f.transmit() // the device catches up and returns every slot
	f.nic.Interrupt()
	if err := f.nic.Send(frame(64, 2)); err != nil || len(f.sent) != bufsMax {
		t.Fatalf("after the slots came back: %v, %d frames on the wire", err, len(f.sent))
	}
	if !f.nic.LinkUp() {
		t.Error("a card without VIRTIO_NET_F_STATUS is down")
	}
}

func TestNoMACIsRefused(t *testing.T) {
	f := virtiotest.New(featStatus, 128)
	if _, err := New(f.Virtio(), f.Mem.Page); err == nil {
		t.Error("a card without an address was accepted")
	}
}
