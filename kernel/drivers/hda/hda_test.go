package hda

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"

	"burrow/kernel/sound"
)

func start(t *testing.T) (*fakeHDA, *Controller, *Codec, Path) {
	t.Helper()
	f := newFakeHDA(graph())
	c, err := NewController(f, f.page)
	if err != nil {
		t.Fatal(err)
	}
	if cs := c.Codecs(); !slices.Equal(cs, []int{0}) {
		t.Fatalf("codecs %v", cs)
	}
	codec, err := ReadCodec(c, 0)
	if err != nil {
		t.Fatal(err)
	}
	p, err := codec.OutputPath()
	if err != nil {
		t.Fatal(err)
	}
	return f, c, codec, p
}

// TestOutputPathSkipsDecoys: the unconnected line out and the speaker that
// reaches no DAC are passed over for the headphones through the mixer.
func TestOutputPathSkipsDecoys(t *testing.T) {
	_, _, codec, p := start(t)
	if codec.Vendor != 0x1af40022 || codec.AFG != 1 || len(codec.Widgets) != 6 {
		t.Fatalf("codec %+v", codec)
	}
	if !slices.Equal(p.Nodes, []int{5, 3, 2}) || !slices.Equal(p.Index, []int{0, 0}) {
		t.Fatalf("path %+v", p)
	}
	if s := codec.String(p); s != "pin 5 (headphones) <- mixer 3 <- dac 2" {
		t.Fatalf("%q", s)
	}
}

// TestSetUpTurnsThePathOn: power, the mixer's input amplifier for the DAC,
// the DAC's output amplifier at its 0 dB step, the pin out with
// headphones and EAPD, the stream tag and format.
func TestSetUpTurnsThePathOn(t *testing.T) {
	f, c, codec, p := start(t)
	format, rate := Format(1<<5|1<<6, 48000)
	if format != 0x0011 || rate != 48000 {
		t.Fatalf("format %#x rate %d", format, rate)
	}
	if err := codec.SetUp(c, p, streamTag, format); err != nil {
		t.Fatal(err)
	}
	want := map[int][]uint32{
		1: {verbSetPower},
		2: {verbSetPower, verbSetAmp | ampOutLR | 0x4a, verbSetStream | streamTag<<4, verbSetFormat | 0x0011},
		3: {verbSetPower, verbSetAmp | ampInLR | 0<<8 | 0x1f},
		5: {verbSetPower, verbSetPinCtl | pinCtlOut | pinCtlHP, verbSetEAPD | eapdOn},
	}
	for nid, w := range want {
		var got []uint32
		for _, v := range f.sent(nid) {
			if v&0xFFF00 != verbGetParam && v&0xFFF00 != verbGetConnList && v&0xFFF00 != verbGetConfig {
				got = append(got, v)
			}
		}
		if !slices.Equal(got, w) {
			t.Errorf("node %d got %#x, want %#x", nid, got, w)
		}
	}
}

func TestConnectionListForms(t *testing.T) {
	nodes := map[int]*fakeNode{9: {params: map[uint32]uint32{paramConnLen: 0x80 | 3}, conns: []int{0x12, 0x8015, 0x20}, long: true}}
	f := newFakeHDA(nodes)
	c, err := NewController(f, f.page)
	if err != nil {
		t.Fatal(err)
	}
	got, err := connections(c, 0, 9)
	if err != nil || !slices.Equal(got, []int{0x12, 0x13, 0x14, 0x15, 0x20}) {
		t.Fatalf("long form with a range: %#x %v", got, err)
	}
}

func TestFormatPicksTheNearestRate(t *testing.T) {
	for _, c := range []struct {
		pcm  uint32
		hz   int
		want int
		fmt  uint16
	}{
		{1<<5 | 1<<6, 44100, 44100, 0x4011},
		{1<<5 | 1<<6, 11025, 44100, 0x4011},
		{1 << 1, 11025, 11025, 0x4311},
		{0x1FC, 8000, 16000, 0x0211},
		{0, 22050, 48000, 0x0011}, // a converter that names no rate: 48 kHz
	} {
		f, hz := Format(c.pcm, c.hz)
		if hz != c.want || f != c.fmt {
			t.Errorf("pcm %#x %d Hz: %#x at %d, want %#x at %d", c.pcm, c.hz, f, hz, c.fmt, c.want)
		}
	}
}

func TestSilentCodecIsAnError(t *testing.T) {
	f := newFakeHDA(graph())
	f.silent = true
	defer func(d time.Duration) { verbWait = d }(verbWait)
	verbWait = 20 * time.Millisecond
	c, err := NewController(f, f.page)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCodec(c, 0); !errors.Is(err, errVerbStall) {
		t.Fatalf("%v", err)
	}
}

func newStream(t *testing.T) (*fakeHDA, *Stream) {
	t.Helper()
	f, c, codec, p := start(t)
	s, err := NewStream(c, codec, p, f.page)
	if err != nil {
		t.Fatal(err)
	}
	if s.Rate() != 48000 || f.R32(regINTCTL)&(intGIE|1<<c.OutputIndex(0)) == 0 {
		t.Fatalf("rate %d, INTCTL %#x", s.Rate(), f.R32(regINTCTL))
	}
	return f, s
}

// TestStreamPlaysAndStops: pages go out in order, each played page is
// zeroed, and the stream stops when nothing is queued.
func TestStreamPlaysAndStops(t *testing.T) {
	f, s := newStream(t)
	base, index := s.base, s.c.OutputIndex(0)
	data := bytes.Repeat([]byte{1, 2, 3, 4}, 3*BufferBytes/4)
	if n, err := s.Write(data); err != nil || n != len(data) {
		t.Fatalf("%d %v", n, err)
	}
	if !f.running(base) || f.R16(base+sdFMT) != 0x0011 || f.R32(base+sdCBL) != Buffers*BufferBytes {
		t.Fatalf("stream not started: CTL %#x FMT %#x", f.R32(base+sdCTL), f.R16(base+sdFMT))
	}
	if free, total, size := s.Space(); free != total-3 || size != BufferBytes {
		t.Fatalf("space %d of %d", free, total)
	}
	f.play(base, index, 1)
	if !s.Interrupt() {
		t.Fatal("the stream's interrupt was not claimed")
	}
	f.ack(index)
	if s.queued != 2 || !bytes.Equal(s.bufs[0], make([]byte, BufferBytes)) || !bytes.Equal(s.bufs[1], data[BufferBytes:2*BufferBytes]) {
		t.Fatalf("after one page: queued %d", s.queued)
	}
	if s.Interrupt() {
		t.Fatal("an interrupt claimed with nothing pending")
	}
	done := make(chan struct{})
	go func() { s.Drain(); close(done) }()
	f.play(base, index, 2)
	s.Interrupt()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Drain did not return")
	}
	if f.running(base) || s.Delay() != 0 {
		t.Fatalf("still running after the last page (CTL %#x)", f.R32(base+sdCTL))
	}
	if !slices.Equal(f.stops, []uint32{streamTag}) {
		t.Fatalf("stops named streams %v: the codec must hear its own stream stop", f.stops)
	}
}

// TestStreamRestartsFromPageZero: after it stopped, the next write starts
// again at page 0 with LPIB reset.
func TestStreamRestartsFromPageZero(t *testing.T) {
	f, s := newStream(t)
	s.Write(make([]byte, BufferBytes))
	f.play(s.base, s.c.OutputIndex(0), 1)
	s.Interrupt()
	if s.running {
		t.Fatal("still running")
	}
	f.W32(s.base+sdLPIB, 0) // the reset clears the position
	s.Write(bytes.Repeat([]byte{9}, BufferBytes))
	if s.head != 1 || s.bufs[0][0] != 9 || !f.running(s.base) {
		t.Fatalf("head %d, page 0 %d", s.head, s.bufs[0][0])
	}
}

// TestStreamCountsFromWhereLPIBStarted: QEMU's reset leaves LPIB where
// it was and clears it only when RUN is set; a second run must not take
// the old position for pages played (it once stopped the stream at once).
func TestStreamCountsFromWhereLPIBStarted(t *testing.T) {
	f, s := newStream(t)
	f.keepLPIB = true
	index := s.c.OutputIndex(0)
	s.Write(make([]byte, 3*BufferBytes))
	f.play(s.base, index, 3)
	s.Interrupt()
	f.ack(index)
	if s.running {
		t.Fatal("still running")
	}
	s.Write(bytes.Repeat([]byte{7}, 2*BufferBytes))
	if free, total, _ := s.Space(); free != total-2 || !f.running(s.base) {
		t.Fatalf("second run: %d of %d free, running %v", free, total, f.running(s.base))
	}
	f.play(s.base, index, 1)
	s.Interrupt()
	if s.queued != 1 || s.bufs[1][0] != 7 {
		t.Fatalf("after one page: queued %d", s.queued)
	}
}

func TestStreamIsADSPOutput(t *testing.T) {
	var _ sound.Output = (*Stream)(nil)
}
