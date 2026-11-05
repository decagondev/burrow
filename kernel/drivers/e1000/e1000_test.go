package e1000

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"burrow/kernel/driver"
	"burrow/kernel/netdev"
)

func start(t *testing.T) (*fakeCard, *NIC) {
	t.Helper()
	c := newFakeCard()
	nic, err := New(c, c.mem.page)
	if err != nil {
		t.Fatal(err)
	}
	done := driver.Serve(c.irq, func() { nic.Interrupt() })
	t.Cleanup(func() { c.irq.Close(); <-done })
	return c, nic
}

func frame(i int) []byte {
	return []byte(fmt.Sprintf("frame %04d %s", i, bytes.Repeat([]byte{'x'}, i%100)))
}

func TestNewSetsUpTheCard(t *testing.T) {
	c, nic := start(t)
	if got := nic.MAC().String(); got != "52:54:00:12:34:56" {
		t.Fatalf("MAC %s", got)
	}
	want := map[uint32]uint32{
		regRAL0: 0x12005452, regRAH0: 0x5634 | rahAV,
		regRDLEN: RingEntries * descBytes, regTDLEN: RingEntries * descBytes,
		regRDT: RingEntries - 1, regTDT: 0, regIMS: intWanted,
	}
	for r, v := range want {
		if got := c.Read(r); got != v {
			t.Errorf("register %#x = %#x, want %#x", r, got, v)
		}
	}
	if c.Read(regRCTL)&rctlEN == 0 || c.Read(regTCTL)&tctlEN == 0 || c.Read(regCTRL)&ctrlSLU == 0 {
		t.Error("receiver, transmitter or link not switched on")
	}
	if !nic.LinkUp() || nic.MTU() != 1500 {
		t.Error("link or MTU")
	}
	var _ netdev.Device = nic
}

func TestSendWrapsAroundTheRing(t *testing.T) {
	c, nic := start(t)
	const count = 3*RingEntries + 5
	for i := 0; i < count; i++ {
		if err := nic.Send(frame(i)); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
	}
	sent := c.sentFrames()
	if len(sent) != count {
		t.Fatalf("the card sent %d frames, want %d", len(sent), count)
	}
	for i, f := range sent {
		if !bytes.Equal(f, frame(i)) {
			t.Fatalf("frame %d is %q", i, f)
		}
	}
	if s := nic.Stats(); s.TxFrames != count || s.TxBusy != 0 {
		t.Fatalf("stats %+v", s)
	}
	if err := nic.Send(make([]byte, maxFrame+1)); err == nil {
		t.Fatal("an oversized frame was queued")
	}
}

func TestSendFullRingIsBusyUntilTheCardCatchesUp(t *testing.T) {
	c, nic := start(t)
	c.mu.Lock()
	c.stalled = true
	c.mu.Unlock()
	for i := 0; i < RingEntries-1; i++ {
		if err := nic.Send(frame(i)); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
	}
	if err := nic.Send(frame(99)); !errors.Is(err, netdev.ErrBusy) {
		t.Fatalf("a full ring answered %v", err)
	}
	c.unstall()
	if err := nic.Send(frame(100)); err != nil {
		t.Fatalf("after the card caught up: %v", err)
	}
	if got := len(c.sentFrames()); got != RingEntries {
		t.Fatalf("the card sent %d frames, want %d", got, RingEntries)
	}
}

func TestReceiveWrapsAroundTheRing(t *testing.T) {
	c, nic := start(t)
	var mu sync.Mutex
	var got [][]byte
	nic.SetReceiver(func(f []byte) { mu.Lock(); got = append(got, f); mu.Unlock() })
	const count = 3*RingEntries + 7
	for i := 0; i < count; i++ {
		deadline := time.Now().Add(2 * time.Second)
		for !c.deliver(frame(i)) { // a full ring waits for the driver to drain it
			if time.Now().After(deadline) {
				t.Fatalf("frame %d: the driver never gave buffers back", i)
			}
			time.Sleep(time.Millisecond)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n == count {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("received %d of %d frames", n, count)
		}
		time.Sleep(time.Millisecond)
	}
	for i, f := range got {
		if !bytes.Equal(f, frame(i)) {
			t.Fatalf("frame %d is %q", i, f)
		}
	}
	if s := nic.Stats(); s.RxFrames != count {
		t.Fatalf("stats %+v", s)
	}
}

func TestReceiveWithoutReceiverDrops(t *testing.T) {
	c, nic := start(t)
	c.deliver(frame(1))
	deadline := time.Now().Add(2 * time.Second)
	for nic.Stats().RxDropped != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("stats %+v", nic.Stats())
		}
		time.Sleep(time.Millisecond)
	}
}

// W22-S01: on a shared line the card claims an interrupt only when its own
// cause register says so; a cause of 0 is another device's interrupt.
func TestInterruptClaimsOnlyItsOwn(t *testing.T) {
	c := newFakeCard()
	nic, err := New(c, c.mem.page)
	if err != nil {
		t.Fatal(err)
	}
	c.reg[regICR] = 0 // whatever start-up raised (link status) is not this test's
	if nic.Interrupt() {
		t.Error("claimed an interrupt with ICR 0")
	}
	c.reg[regICR] = intTXDW
	if !nic.Interrupt() {
		t.Error("did not claim its own TX interrupt")
	}
	if c.reg[regICR] != 0 {
		t.Error("ICR not cleared by the read")
	}
}
