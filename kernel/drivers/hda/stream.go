// stream.go is the output stream as sound.Output (spec §3.3.35-3.3.44,
// §3.6.2): a buffer descriptor list of Buffers pages, each with its
// interrupt on completion, that the controller plays round and round once
// RUN is set. Unlike the AC'97's, the stream never halts by itself, so the
// driver keeps the pages it has not filled silent: a page is zeroed as
// soon as it has played, where it is played is read from LPIB, and when
// nothing is queued any more the stream is stopped and reset, to start
// again from page 0 with the next write. One buffer stays free, so the
// writer never fills the page being played. Positions are LPIB less what
// it read just after the stream started: a reset clears LPIB on real
// controllers, QEMU's keeps it and clears it when RUN is set, and either
// way the ring starts again from page 0.
package hda

import (
	"errors"
	"sync"
	"time"
)

const (
	Buffers     = 32
	BufferBytes = pageBytes // 1024 frames of 16-bit stereo
	stallLimit  = 5 * time.Second
)

var ErrStalled = errors.New("hda: the stream stopped taking samples")

// Stream is one output stream on a path.
type Stream struct {
	c     *Controller
	base  uint32
	codec *Codec
	path  Path
	pcm   uint32 // the DAC's PCM parameter
	bdl   []byte
	bdlPA uint64
	bufs  [Buffers][]byte
	lpib0 uint32 // LPIB just after the stream started

	mu      sync.Mutex
	cond    *sync.Cond
	rate    int
	format  uint16
	stage   []byte
	head    int // the next page to fill
	play    int // the page the controller is playing
	queued  int // pages filled and not yet played
	running bool
	changed chan struct{}
}

// NewStream sets up output stream 0 for codec's path in pages from page.
func NewStream(c *Controller, codec *Codec, path Path, page DMAPage) (*Stream, error) {
	s := &Stream{c: c, base: c.OutputBase(0), codec: codec, path: path, changed: make(chan struct{}),
		stage: make([]byte, 0, BufferBytes)}
	s.cond = sync.NewCond(&s.mu)
	s.pcm, _ = c.Verb(codec.CAd, path.Nodes[len(path.Nodes)-1], verbGetParam|paramPCM)
	if s.pcm&0x7FF == 0 {
		s.pcm, _ = c.Verb(codec.CAd, codec.AFG, verbGetParam|paramPCM)
	}
	bdlPA, bdl, err := page()
	if err != nil {
		return nil, err
	}
	s.bdl, s.bdlPA = bdl, bdlPA
	for i := range s.bufs {
		pa, mem, err := page()
		if err != nil {
			return nil, err
		}
		s.bufs[i] = mem
		e := bdl[i*bdlEntry:]
		le.PutUint64(e, pa)
		le.PutUint32(e[8:], BufferBytes)
		le.PutUint32(e[12:], bdlIOC)
	}
	s.format, s.rate = Format(s.pcm, defaultHz)
	if err := codec.SetUp(c, path, streamTag, s.format); err != nil {
		return nil, err
	}
	s.resetLocked()
	c.r.W32(regINTCTL, c.r.R32(regINTCTL)|intGIE|1<<c.OutputIndex(0))
	return s, nil
}

// resetLocked stops the stream and resets its descriptor, then writes it
// again (a reset clears it): the BDL, its length and last index, the
// format and the tag, with completion interrupts on. RUN is cleared with
// the stream number kept: the controller tells the codecs that stream
// number stopped (QEMU's takes it from the value written, and a codec
// never told goes on thinking its stream runs).
func (s *Stream) resetLocked() {
	r := s.c.r
	r.W32(s.base+sdCTL, r.R32(s.base+sdCTL)&^ctlRUN&0xFFFFFF)
	waitFor(resetWait, func() bool { return r.R32(s.base+sdCTL)&ctlRUN == 0 })
	r.W32(s.base+sdCTL, ctlSRST)
	waitFor(resetWait, func() bool { return r.R32(s.base+sdCTL)&ctlSRST != 0 })
	r.W32(s.base+sdCTL, 0)
	waitFor(resetWait, func() bool { return r.R32(s.base+sdCTL)&ctlSRST == 0 })
	r.W32(s.base+sdBDPL, uint32(s.bdlPA))
	r.W32(s.base+sdBDPU, uint32(s.bdlPA>>32))
	r.W32(s.base+sdCBL, Buffers*BufferBytes)
	r.W16(s.base+sdLVI, Buffers-1)
	r.W16(s.base+sdFMT, s.format)
	r.W32(s.base+sdCTL, streamTag<<ctlStrmSh|ctlIOCE)
	r.W8(s.base+sdSTS, stsBCIS|stsErrors)
	for _, b := range s.bufs {
		clear(b)
	}
	s.running, s.head, s.play, s.queued = false, 0, 0, 0
}

// Rate is the rate the stream plays at.
func (s *Stream) Rate() int { s.mu.Lock(); defer s.mu.Unlock(); return s.rate }

// SetRate takes the nearest rate the converter has; what is queued is
// dropped.
func (s *Stream) SetRate(hz int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	format, rate := Format(s.pcm, hz)
	if format != s.format {
		s.format, s.rate = format, rate
		s.resetLocked()
		s.stage = s.stage[:0]
		s.codec.SetFormat(s.c, s.path, streamTag, format)
		s.wakeLocked()
	}
	return s.rate
}

// syncLocked moves play to where LPIB is, zeroing and freeing the pages
// played since; with nothing left queued the stream stops.
func (s *Stream) syncLocked() {
	if !s.running {
		return
	}
	cur := s.position() / BufferBytes
	done := (cur - s.play + Buffers) % Buffers
	for ; done > 0; done-- {
		clear(s.bufs[s.play])
		s.play = (s.play + 1) % Buffers
		if s.queued > 0 {
			s.queued--
		}
	}
	if s.queued == 0 {
		s.resetLocked()
	}
}

// position is the byte the stream is at in its ring, since it started.
func (s *Stream) position() int {
	return int((s.c.r.R32(s.base+sdLPIB) - s.lpib0) % (Buffers * BufferBytes))
}

func (s *Stream) waitLocked(deadline time.Time) bool {
	left := time.Until(deadline)
	if left <= 0 {
		return false
	}
	t := time.AfterFunc(left, func() { s.mu.Lock(); s.cond.Broadcast(); s.mu.Unlock() })
	s.cond.Wait()
	t.Stop()
	return true
}

// submitLocked hands the staged frames over as the next page, starting the
// stream if it is stopped.
func (s *Stream) submitLocked() error {
	deadline := time.Now().Add(stallLimit)
	for s.syncLocked(); s.queued >= Buffers-1; s.syncLocked() {
		if !s.waitLocked(deadline) {
			return ErrStalled
		}
	}
	copy(s.bufs[s.head], s.stage)
	clear(s.bufs[s.head][len(s.stage):])
	s.head = (s.head + 1) % Buffers
	s.queued++
	s.stage = s.stage[:0]
	if !s.running {
		s.running = true
		r := s.c.r
		r.W32(s.base+sdCTL, r.R32(s.base+sdCTL)|ctlRUN) // after the page: the store orders it
		s.lpib0 = r.R32(s.base + sdLPIB)
	}
	return nil
}

// Write queues 16-bit stereo frames, blocking while the ring is full.
func (s *Stream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for len(p) > 0 {
		k := min(len(p), BufferBytes-len(s.stage))
		s.stage = append(s.stage, p[:k]...)
		p, n = p[k:], n+k
		if len(s.stage) == BufferBytes {
			if err := s.submitLocked(); err != nil {
				return n, err
			}
		}
	}
	return n, nil
}

// Drain hands over a partial page and waits until everything has played.
func (s *Stream) Drain() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.stage) > 0 && s.submitLocked() != nil {
		return
	}
	deadline := time.Now().Add(stallLimit)
	for s.syncLocked(); s.running; s.syncLocked() {
		if !s.waitLocked(deadline) {
			return
		}
	}
}

// Reset stops playback and drops everything queued.
func (s *Stream) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetLocked()
	s.stage = s.stage[:0]
	s.wakeLocked()
}

// Space: free pages, how many a writer may fill, bytes in one.
func (s *Stream) Space() (free, total, size int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncLocked()
	return Buffers - 1 - s.queued, Buffers - 1, BufferBytes
}

// Delay is the bytes written and not yet played.
func (s *Stream) Delay() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncLocked()
	if !s.running {
		return len(s.stage)
	}
	in := s.position() % BufferBytes
	return max(0, s.queued*BufferBytes-in) + len(s.stage)
}

// Changed is closed when a page is freed.
func (s *Stream) Changed() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changed
}

func (s *Stream) wakeLocked() {
	s.cond.Broadcast()
	close(s.changed)
	s.changed = make(chan struct{})
}

// Interrupt serves the controller and says whether it raised the
// interrupt: a page completed (or the stream hit an error).
func (s *Stream) Interrupt() (mine bool) {
	r := s.c.r
	if r.R32(regINTSTS)&(1<<s.c.OutputIndex(0)) == 0 {
		return false
	}
	sts := r.R8(s.base + sdSTS)
	r.W8(s.base+sdSTS, sts&(stsBCIS|stsErrors))
	s.mu.Lock()
	s.syncLocked()
	s.wakeLocked()
	s.mu.Unlock()
	return true
}
