package ahci

// ahci_test.go - the driver against a fake HBA: ports 0 and 2 implemented
// with an ATA disk on 2 (and 0 empty), started the way a firmware leaves
// it (running, on its own lists); commands run when CI is written, the way
// AHCI says, from the command list and table in the driver's DMA pages,
// and the interrupt fires. Then blocktest's conformance, a write that
// fails with a task file error, and a flush.

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"burrow/kernel/block"
	"burrow/kernel/block/blocktest"
	"burrow/kernel/virtio/virtiotest"
)

type fakeHBA struct {
	mu      sync.Mutex
	regs    map[uint32]uint32
	mem     virtiotest.Memory
	image   []byte
	failLBA int64
	flushes atomic.Int32
	hba     atomic.Pointer[HBA]
}

const diskPort = 2

func pbase(n int) uint32 { return uint32(portBase + n*portBytes) }

func newFake(sectors int) *fakeHBA {
	f := &fakeHBA{regs: map[uint32]uint32{}, image: blocktest.Pattern(sectors, sectorBytes), failLBA: -1}
	f.regs[hbaCAP] = capS64A | 5
	f.regs[hbaPI] = 1<<0 | 1<<diskPort
	b := pbase(diskPort)
	f.regs[b+pxSSTS] = 0x113
	f.regs[b+pxSIG] = sigATA
	f.regs[b+pxTFD] = 0x50
	f.regs[b+pxCMD] = cmdST | cmdFRE | cmdCR | cmdFR // the firmware's
	return f
}

func (f *fakeHBA) R32(off uint32) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.regs[off]
}

func (f *fakeHBA) W32(off uint32, v uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := pbase(diskPort)
	switch {
	case off == hbaIS || off == b+pxIS || off == b+pxSERR: // write 1 to clear
		f.regs[off] &^= v
	case off == b+pxCMD:
		v &^= cmdCR | cmdFR
		if v&cmdST != 0 {
			v |= cmdCR
		}
		if v&cmdFRE != 0 {
			v |= cmdFR
		}
		f.regs[off] = v
	case off == b+pxCI:
		f.regs[off] |= v
		go f.execute()
	default:
		f.regs[off] = v
	}
}

// execute runs slot 0 as the HBA would and raises the port's interrupt.
func (f *fakeHBA) execute() {
	f.mu.Lock()
	b := pbase(diskPort)
	clb := uint64(f.regs[b+pxCLB]) | uint64(f.regs[b+pxCLBU])<<32
	f.mu.Unlock()
	hdr := f.mem.At(clb, 32)
	ct := uint64(le.Uint32(hdr[8:])) | uint64(le.Uint32(hdr[12:]))<<32
	prdtl := int(le.Uint32(hdr[0:]) >> 16)
	cfis := f.mem.At(ct, 64)
	lba := uint64(cfis[4]) | uint64(cfis[5])<<8 | uint64(cfis[6])<<16 | uint64(cfis[8])<<24 | uint64(cfis[9])<<32 | uint64(cfis[10])<<40
	pos := int(lba) * sectorBytes
	failed := false
	switch cfis[2] {
	case ataIdentify:
		id := f.mem.At(le.Uint64(f.mem.At(ct+prdtOff, 8)), sectorBytes)
		le.PutUint16(id[2*identifyLBA4:], 1<<10)
		le.PutUint64(id[200:], uint64(len(f.image)/sectorBytes))
		copy(id[54:], "EQUMH  ")
	case ataFlushExt:
		f.flushes.Add(1)
	default:
		for k := 0; k < prdtl; k++ {
			e := f.mem.At(ct+prdtOff+uint64(k*prdtBytes), prdtBytes)
			n := int(le.Uint32(e[12:])&0x3FFFFF) + 1
			buf := f.mem.At(le.Uint64(e[0:]), n)
			if f.failLBA >= 0 && int64(lba) <= f.failLBA && f.failLBA < int64(lba)+int64(cfis[12]) {
				failed = true
			} else if cfis[2] == ataReadExt {
				copy(buf, f.image[pos:])
			} else {
				copy(f.image[pos:], buf)
			}
			pos += n
		}
	}
	f.mu.Lock()
	f.regs[b+pxCI] = 0
	f.regs[b+pxIS] |= 1
	if failed {
		f.regs[b+pxIS] |= isTFES
		f.regs[b+pxTFD] = 0x51
	}
	f.regs[hbaIS] |= 1 << diskPort
	f.mu.Unlock()
	if h := f.hba.Load(); h != nil {
		h.Interrupt()
	}
}

func setUp(t *testing.T, sectors int) (*fakeHBA, *Port) {
	t.Helper()
	f := newFake(sectors)
	h, errs := New(f, f.mem.Page)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	f.hba.Store(h)
	if errs := h.Identify(); len(errs) != 0 {
		t.Fatal(errs)
	}
	if len(h.Ports) != 1 || h.Ports[0].Number != diskPort || !h.Wide {
		t.Fatalf("ports %v", h.Ports)
	}
	if f.R32(hbaGHC)&(ghcAE|ghcIE) != ghcAE|ghcIE {
		t.Errorf("GHC %#x: want AHCI mode and interrupts", f.R32(hbaGHC))
	}
	return f, h.Ports[0]
}

func TestIdentifyAndConformance(t *testing.T) {
	f, p := setUp(t, 600)
	if p.Sectors() != 600 || p.Model != "QEMU H" {
		t.Errorf("sectors %d, model %q", p.Sectors(), p.Model)
	}
	blocktest.Run(t, p, append([]byte(nil), f.image...)) // 600 sectors: several 128-sector commands
	blocktest.RunWriter(t, p)
	if f.flushes.Load() == 0 {
		t.Error("RunWriter's flushes never reached the disk")
	}
}

func TestTaskFileError(t *testing.T) {
	f, p := setUp(t, 64)
	f.failLBA = 10
	err := p.WriteSectors(8, make([]byte, 4*sectorBytes))
	if err == nil {
		t.Fatal("a failed write succeeded")
	}
	f.failLBA = -1
	if err := p.ReadSectors(8, make([]byte, 4*sectorBytes)); err != nil {
		t.Errorf("the port after an error: %v", err)
	}
	if !errors.Is(p.ReadSectors(64, make([]byte, sectorBytes)), block.ErrRange) {
		t.Error("a read past the end was not refused")
	}
}
