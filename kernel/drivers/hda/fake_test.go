package hda

// fakeHDA plays an HD Audio controller for host tests: its registers, DMA
// pages by physical address, a CORB the test's codec answers into the
// RIRB as soon as the write pointer moves, and an output stream whose
// position the test moves by "playing" pages.

import (
	"sync"
)

type fakeNode struct {
	params map[uint32]uint32 // GET_PARAMETER answers
	conns  []int             // connection list (short form)
	long   bool
	config uint32
}

type fakeHDA struct {
	mu       sync.Mutex
	reg      map[uint32]uint32
	pages    map[uint64][]byte
	next     uint64
	stops    []uint32 // the stream number each RUN 1->0 write carried
	nodes    map[int]*fakeNode
	verbs    []uint32 // every verb a node got, as cmd words
	silent   bool     // the codec never answers
	keepLPIB bool     // QEMU's controller: a stream reset keeps LPIB, RUN clears it
}

func newFakeHDA(nodes map[int]*fakeNode) *fakeHDA {
	return &fakeHDA{reg: map[uint32]uint32{regGCAP: 0x4401, regCORBSIZE: 0x42, regRIRBSIZE: 0x42},
		pages: map[uint64][]byte{}, next: 0x20_0000, nodes: nodes}
}

func (f *fakeHDA) page() (uint64, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pa := f.next
	f.next += pageBytes
	f.pages[pa] = make([]byte, pageBytes)
	return pa, f.pages[pa], nil
}

func (f *fakeHDA) R8(off uint32) uint8   { return uint8(f.R32(off)) }
func (f *fakeHDA) R16(off uint32) uint16 { return uint16(f.R32(off)) }
func (f *fakeHDA) R32(off uint32) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reg[off]
}
func (f *fakeHDA) W8(off uint32, v uint8)   { f.write(off, uint32(v)) }
func (f *fakeHDA) W16(off uint32, v uint16) { f.write(off, uint32(v)) }
func (f *fakeHDA) W32(off uint32, v uint32) { f.write(off, v) }

func (f *fakeHDA) write(off, v uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case off == regGCTL:
		if v&gctlCRST != 0 && f.reg[regGCTL]&gctlCRST == 0 {
			f.reg[regSTATESTS] = 1 // codec 0 answers the reset
		}
	case off == regSTATESTS || off == regRIRBSTS:
		f.reg[off] &^= v
		return
	case off == regCORBRP:
		v = 0
	case off == regRIRBWP:
		if v&rirbWPRST != 0 {
			v = 0
		}
	case off == regCORBWP:
		f.reg[off] = v
		f.serveCORB()
		return
	case off >= streamBase && (off-streamBase)%streamBytes == sdSTS:
		f.reg[off] &^= v
		return
	case off >= streamBase && (off-streamBase)%streamBytes == sdCTL && v&ctlRUN == 0 && f.reg[off]&ctlRUN != 0:
		f.stops = append(f.stops, v>>ctlStrmSh&0xF) // as QEMU tells its codecs
	case off >= streamBase && (off-streamBase)%streamBytes == sdCTL && v&ctlSRST != 0 && !f.keepLPIB:
		f.reg[off-sdCTL+sdLPIB] = 0 // a stream reset clears its position
	case off >= streamBase && (off-streamBase)%streamBytes == sdCTL && f.keepLPIB &&
		v&ctlRUN != 0 && f.reg[off]&ctlRUN == 0:
		f.reg[off-sdCTL+sdLPIB] = 0 // QEMU: the position starts again with RUN
	}
	f.reg[off] = v
}

// serveCORB answers every verb from the read pointer to the write pointer.
func (f *fakeHDA) serveCORB() {
	corb := f.at(uint64(f.reg[regCORBLBASE]) | uint64(f.reg[regCORBUBASE])<<32)
	rirb := f.at(uint64(f.reg[regRIRBLBASE]) | uint64(f.reg[regRIRBUBASE])<<32)
	for rp := f.reg[regCORBRP]; rp != f.reg[regCORBWP]; {
		rp = (rp + 1) % 256
		cmd := le.Uint32(corb[4*rp:])
		f.verbs = append(f.verbs, cmd)
		f.reg[regCORBRP] = rp
		if f.silent {
			continue
		}
		wp := (f.reg[regRIRBWP] + 1) % 256
		le.PutUint32(rirb[8*wp:], f.answer(cmd))
		le.PutUint32(rirb[8*wp+4:], cmd>>28)
		f.reg[regRIRBWP] = wp
	}
}

func (f *fakeHDA) at(pa uint64) []byte {
	base := pa &^ (pageBytes - 1)
	return f.pages[base][pa-base:]
}

func (f *fakeHDA) answer(cmd uint32) uint32 {
	n, ok := f.nodes[int(cmd>>20)&0x7F]
	if !ok {
		return 0
	}
	verb := cmd & 0xFFFFF
	switch {
	case verb&0xFFF00 == verbGetParam:
		return n.params[verb&0xFF]
	case verb&0xFFF00 == verbGetConfig:
		return n.config
	case verb&0xFFF00 == verbGetConnList:
		i := int(verb & 0xFF)
		var r uint32
		per, bits := 4, 8
		if n.long {
			per, bits = 2, 16
		}
		for j := 0; j < per && i+j < len(n.conns); j++ {
			r |= uint32(n.conns[i+j]) << (bits * j)
		}
		return r
	}
	return 0
}

// play moves the stream at base on by pages pages, as the controller would
// after playing them, and raises its completion interrupt.
func (f *fakeHDA) play(base uint32, index, pages int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	lpib := (f.reg[base+sdLPIB] + uint32(pages*BufferBytes)) % (Buffers * BufferBytes)
	f.reg[base+sdLPIB] = lpib
	f.reg[base+sdSTS] |= stsBCIS
	f.reg[regINTSTS] |= 1 << index
}

func (f *fakeHDA) ack(index int) {
	f.mu.Lock()
	f.reg[regINTSTS] &^= 1 << index
	f.mu.Unlock()
}

func (f *fakeHDA) running(base uint32) bool { return f.R32(base+sdCTL)&ctlRUN != 0 }

// sent lists the verbs node nid got (their low 20 bits).
func (f *fakeHDA) sent(nid int) []uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []uint32
	for _, c := range f.verbs {
		if int(c>>20)&0x7F == nid {
			out = append(out, c&0xFFFFF)
		}
	}
	return out
}

// graph is a codec with a decoy: an unconnected line out (4), a speaker (7)
// that reaches only an ADC, and headphones (5) through a mixer (3) to the
// DAC (2).
func graph() map[int]*fakeNode {
	widget := func(typ, caps uint32) map[uint32]uint32 {
		return map[uint32]uint32{paramWidgetCaps: typ<<20 | caps}
	}
	nodes := map[int]*fakeNode{
		0: {params: map[uint32]uint32{paramVendor: 0x1af40022, paramNodes: 1<<16 | 1}},
		1: {params: map[uint32]uint32{paramFuncType: funcAudio, paramNodes: 2<<16 | 6,
			paramOutAmpCaps: 0x2a, paramPCM: 1<<17 | 1<<5 | 1<<6}},
		2: {params: widget(TypeOutput, capOutAmp|capAmpOverride)},
		3: {params: widget(TypeMixer, capInAmp), conns: []int{2, 6}},
		4: {params: widget(TypePin, capOutAmp), conns: []int{2}, config: 1<<30 | 0<<20},
		5: {params: widget(TypePin, 0), conns: []int{3}, config: 2 << 20},
		6: {params: widget(TypeInput, 0)},
		7: {params: widget(TypePin, 0), conns: []int{6}, config: 1 << 20},
	}
	nodes[2].params[paramOutAmpCaps] = 0x4a
	nodes[1].params[paramInAmpCaps] = 0x1f
	for _, n := range []int{4, 5, 7} {
		nodes[n].params[paramPinCaps] = pinCapOut
	}
	nodes[5].params[paramPinCaps] |= pinCapEAPD
	for nid, n := range nodes {
		n.params[paramConnLen] = uint32(len(n.conns))
		_ = nid
	}
	return nodes
}
