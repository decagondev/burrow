package ac97

import (
	"bytes"
	"encoding/binary"
	"sync"
	"testing"
	"time"
)

const fakeNAM, fakeNABM = 0x1000, 0x2000

// fake is an AC'97 as the driver sees it: the codec's registers, the
// PCM-out box, DMA pages by physical address, and a bus master that plays
// one descriptor per play() call, as the card does at its rate.
type fake struct {
	mu       sync.Mutex
	mixer    map[uint16]uint16
	glob     uint32
	civ, lvi uint8
	sr       uint16
	cr       uint8
	bdbar    uint32
	running  bool
	pages    map[uint64][]byte
	nextPA   uint64
	played   bytes.Buffer
	restarts int
}

func newFake() *fake {
	return &fake{mixer: map[uint16]uint16{namExtID: extVRA}, pages: map[uint64][]byte{}, nextPA: 0x100000, sr: srDCH}
}

func (f *fake) page() (uint64, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pa := f.nextPA
	f.nextPA += 4096
	f.pages[pa] = make([]byte, 4096)
	return pa, f.pages[pa], nil
}

func (f *fake) In8(port uint16) uint8 {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch port - fakeNABM {
	case poCIV:
		return f.civ
	case poLVI:
		return f.lvi
	case poCR:
		return f.cr
	}
	return 0
}

func (f *fake) Out8(port uint16, v uint8) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch port - fakeNABM {
	case poLVI:
		f.lvi = v & (Buffers - 1)
	case poCR:
		if v&crRR != 0 {
			f.civ, f.lvi, f.sr, f.cr, f.running = 0, 0, srDCH, 0, false
			return
		}
		f.cr = v
		if v&crRun != 0 && !f.running {
			f.running, f.sr = true, f.sr&^srDCH
			f.restarts++
		}
		if v&crRun == 0 {
			f.running = false
		}
	}
}

func (f *fake) In16(port uint16) uint16 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if port >= fakeNABM {
		if port-fakeNABM == poSR {
			return f.sr
		}
		return 0 // PICB: the buffer playing is played at once here
	}
	r := port - fakeNAM
	if r == namFrontRate && f.mixer[r] == 0 {
		return 48000
	}
	return f.mixer[r]
}

func (f *fake) Out16(port uint16, v uint16) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if port >= fakeNABM {
		if port-fakeNABM == poSR {
			f.sr &^= v & srIRQs
		}
		return
	}
	r := port - fakeNAM
	if r == namFrontRate && v > 44100 && v != 48000 {
		v = 48000 // the codec rounds a rate it cannot do
	}
	f.mixer[r] = v
}

func (f *fake) In32(port uint16) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if port-fakeNABM == globSta && f.glob&globColdOff != 0 {
		return staCodecRdy
	}
	return 0
}

func (f *fake) Out32(port uint16, v uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch port - fakeNABM {
	case globCnt:
		f.glob = v
	case poBDBAR:
		f.bdbar = v
	}
}

// play plays the descriptor at CIV, as the bus master does: its samples go
// to played, BCIS is set, and after the last valid one the channel halts.
// It returns false when nothing is running.
func (f *fake) play() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.running {
		return false
	}
	d := f.pages[uint64(f.bdbar)][int(f.civ)*descBytes:]
	buf := f.pages[uint64(binary.LittleEndian.Uint32(d))]
	f.played.Write(buf[:2*int(binary.LittleEndian.Uint16(d[4:]))])
	f.sr |= srBCIS
	if f.civ == f.lvi {
		f.sr |= srLVBCI | srDCH
		f.running = false
	} else {
		f.civ = (f.civ + 1) & (Buffers - 1)
	}
	return true
}

func setup(t *testing.T) (*fake, *Card) {
	t.Helper()
	f := newFake()
	c, err := New(Ports{IO8: f, IO16: f, IO32: f, NAM: fakeNAM, NABM: fakeNABM}, f.page)
	if err != nil {
		t.Fatal(err)
	}
	return f, c
}

// player plays as the card does, raising its interrupt after each buffer,
// until stop closes.
func player(f *fake, c *Card, stop <-chan struct{}) *sync.WaitGroup {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if f.play() {
				c.Interrupt()
			} else {
				time.Sleep(100 * time.Microsecond)
			}
		}
	}()
	return &wg
}

func pattern(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7) + seed
	}
	return b
}

// W22-S02: three rings' worth of samples, written in odd-sized pieces while
// the card plays, come out of it in order and complete; the writer blocks
// while the ring is full rather than overwriting what is playing.
func TestRingPlaysEverythingInOrder(t *testing.T) {
	f, c := setup(t)
	if !c.VRA() || c.SetRate(22050) != 22050 || c.SetRate(96000) != 48000 {
		t.Fatal("VRA and rates")
	}
	stop := make(chan struct{})
	wg := player(f, c, stop)
	want := pattern(3*Buffers*BufferBytes+1000, 1) // a partial last buffer
	for p := want; len(p) > 0; {
		k := min(len(p), 3001)
		if n, err := c.Write(p[:k]); n != k || err != nil {
			t.Fatalf("Write: %d, %v", n, err)
		}
		p = p[k:]
	}
	c.Drain()
	close(stop)
	wg.Wait()
	if got := f.played.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("played %d bytes, want %d (first difference at %d)", len(got), len(want), firstDiff(got, want))
	}
	if free, total, size := c.Space(); free != total || size != BufferBytes || c.Delay() != 0 {
		t.Errorf("after Drain: Space %d/%d, Delay %d", free, total, c.Delay())
	}
}

// After the card halts with everything played (an underrun, or a pause),
// the next write starts it again from descriptor 0.
func TestRestartAfterHalt(t *testing.T) {
	f, c := setup(t)
	stop := make(chan struct{})
	wg := player(f, c, stop)
	first, second := pattern(BufferBytes, 3), pattern(5*BufferBytes, 9)
	c.Write(first)
	c.Drain()
	c.Write(second)
	c.Drain()
	close(stop)
	wg.Wait()
	if !bytes.Equal(f.played.Bytes(), append(first, second...)) {
		t.Errorf("played %d bytes, want %d", f.played.Len(), len(first)+len(second))
	}
	if f.restarts != 2 {
		t.Errorf("started %d times, want 2", f.restarts)
	}
}

// With nobody playing, a writer gets the ring less one descriptor, then
// blocks; Reset drops what is queued.
func TestFullRingAndReset(t *testing.T) {
	_, c := setup(t)
	if _, err := c.Write(pattern((Buffers-1)*BufferBytes, 0)); err != nil {
		t.Fatal(err)
	}
	if free, total, _ := c.Space(); free != 0 || total != Buffers-1 {
		t.Errorf("Space %d/%d, want 0 free", free, total)
	}
	if d := c.Delay(); d != (Buffers-2)*BufferBytes {
		t.Errorf("Delay %d", d)
	}
	done := make(chan struct{})
	go func() { c.Write(pattern(BufferBytes, 0)); close(done) }()
	select {
	case <-done:
		t.Fatal("Write did not block on a full ring")
	case <-time.After(50 * time.Millisecond):
	}
	c.Reset()
	<-done // the reset frees the ring
	if c.Interrupt() {
		t.Error("claimed an interrupt with no status bit set")
	}
}

func firstDiff(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}
