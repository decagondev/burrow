package usb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"
	"testing"

	"burrow/kernel/block/blocktest"
)

// fakeStick is a bulk-only device over a RAM disk: the OUT pipe takes
// command wrappers and written data, the IN pipe gives read data and
// status wrappers. stallReads makes the next READ's data stage stall once
// (after which the status says "failed"); gone fails every transfer.
type fakeStick struct {
	mu         sync.Mutex
	disk       []byte
	notReady   int // TEST UNIT READYs that fail first
	stallReads int
	gone       bool
	cleared    []uint16 // endpoints CLEAR_FEATURE(HALT) named
	resets     int

	pending []byte // what the IN pipe gives next
	writeTo int    // where OUT data goes (-1: a wrapper is expected)
	writeN  int
	tag     uint32
}

var errGone = errors.New("transaction error")

func newStick(sectors int) *fakeStick {
	disk := make([]byte, sectors*512)
	for i := range disk {
		disk[i] = byte(i*7 + i/512)
	}
	return &fakeStick{disk: disk, writeTo: -1}
}

func (f *fakeStick) Control(s Setup, data []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gone {
		return 0, errGone
	}
	switch s.Request {
	case reqClearFeat:
		f.cleared = append(f.cleared, s.Index)
	case reqBOTReset:
		f.resets++
		f.pending, f.writeTo = nil, -1
	}
	return 0, nil
}

func (f *fakeStick) Open(ep Endpoint) (Pipe, error) {
	if ep.In() {
		return pipeFunc(f.read), nil
	}
	return pipeFunc(f.write), nil
}

func csw(tag uint32, status byte) []byte {
	b := make([]byte, cswBytes)
	binary.LittleEndian.PutUint32(b[0:], cswSig)
	binary.LittleEndian.PutUint32(b[4:], tag)
	b[12] = status
	return b
}

func (f *fakeStick) write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gone {
		return 0, errGone
	}
	if f.writeTo >= 0 { // the data stage of a WRITE(10)
		n := copy(f.disk[f.writeTo:f.writeTo+f.writeN], b)
		f.writeTo += n
		if f.writeN -= n; f.writeN == 0 {
			f.writeTo, f.pending = -1, csw(f.tag, cswPassed)
		}
		return n, nil
	}
	if len(b) != cbwBytes || binary.LittleEndian.Uint32(b) != cbwSig {
		return 0, ErrStall
	}
	f.tag = binary.LittleEndian.Uint32(b[4:])
	cb := b[15 : 15+b[14]]
	f.pending = f.answer(cb)
	return len(b), nil
}

// answer is what the IN pipe gives for a command: data, then the status.
func (f *fakeStick) answer(cb []byte) []byte {
	ok := csw(f.tag, cswPassed)
	switch cb[0] {
	case scsiInquiry:
		inq := make([]byte, 36)
		copy(inq[8:], "QEMU    QEMU HARDDISK   ")
		return append(inq, ok...)
	case scsiTestUnitReady:
		if f.notReady > 0 {
			f.notReady--
			return csw(f.tag, cswFailed)
		}
		return ok
	case scsiRequestSense:
		return append(make([]byte, 18), ok...)
	case scsiReadCapacity:
		c := make([]byte, 8)
		binary.BigEndian.PutUint32(c, uint32(len(f.disk)/512-1))
		binary.BigEndian.PutUint32(c[4:], 512)
		return append(c, ok...)
	case scsiRead10, scsiWrite10:
		lba := int(binary.BigEndian.Uint32(cb[2:]))
		n := int(binary.BigEndian.Uint16(cb[7:])) * 512
		if cb[0] == scsiWrite10 {
			f.writeTo, f.writeN = lba*512, n
			return nil
		}
		if f.stallReads > 0 {
			f.stallReads--
			return nil // the data stage stalls; the status says failed
		}
		return append(append([]byte(nil), f.disk[lba*512:lba*512+n]...), ok...)
	}
	return ok
}

func (f *fakeStick) read(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gone {
		return 0, errGone
	}
	if f.pending == nil {
		f.pending = csw(f.tag, cswFailed)
		return 0, ErrStall
	}
	if len(f.pending) == cswBytes && len(b) != cswBytes {
		return 0, nil // a zero-length packet: the data ended early
	}
	n := copy(b, f.pending)
	f.pending = f.pending[n:]
	if len(f.pending) == 0 {
		f.pending = nil
	}
	return n, nil
}

var stickInterface = Interface{Number: 0, Class: ClassStorage, SubClass: subclassSCSI, Protocol: protocolBOT,
	Endpoints: []Endpoint{{Address: 0x81, Attributes: XferBulk, MaxPacket: 512}, {Address: 0x02, Attributes: XferBulk, MaxPacket: 512}}}

func TestStorageConformance(t *testing.T) {
	f := newStick(300)
	f.notReady = 2
	want := append([]byte(nil), f.disk...)
	k, err := OpenStorage(f, stickInterface)
	if err != nil {
		t.Fatal(err)
	}
	if k.Model != "QEMU QEMU HARDDISK" || k.Sectors() != 300 || k.SectorSize() != 512 {
		t.Fatalf("model %q, %d sectors of %d", k.Model, k.Sectors(), k.SectorSize())
	}
	blocktest.Run(t, k, want)
}

func TestStorageWriteAndFlush(t *testing.T) {
	f := newStick(300)
	k, err := OpenStorage(f, stickInterface)
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("burrow!!"), 160*512/8) // 160 sectors: three commands
	if err := k.WriteSectors(100, data); err != nil {
		t.Fatal(err)
	}
	if err := k.Flush(); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(data))
	if err := k.ReadSectors(100, got); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("read back: %v", err)
	}
	if err := k.WriteSectors(299, make([]byte, 1024)); err == nil {
		t.Fatal("a write past the end was accepted")
	}
}

func TestStorageStallRecovered(t *testing.T) {
	f := newStick(64)
	k, err := OpenStorage(f, stickInterface)
	if err != nil {
		t.Fatal(err)
	}
	f.stallReads = 1
	buf := make([]byte, 512)
	if err := k.ReadSectors(3, buf); !errors.Is(err, errCommand) {
		t.Fatalf("a stalled read: %v", err)
	}
	if len(f.cleared) != 1 || f.cleared[0] != 0x81 {
		t.Fatalf("cleared %#x", f.cleared)
	}
	if err := k.ReadSectors(3, buf); err != nil || !bytes.Equal(buf, f.disk[3*512:4*512]) {
		t.Fatalf("the next read: %v", err)
	}
}

func TestStorageGone(t *testing.T) {
	f := newStick(64)
	k, err := OpenStorage(f, stickInterface)
	if err != nil {
		t.Fatal(err)
	}
	f.gone = true
	if err := k.ReadSectors(0, make([]byte, 512)); err == nil {
		t.Fatal("a read from a removed stick succeeded")
	}
	if err := k.WriteSectors(0, make([]byte, 512)); err == nil {
		t.Fatal("a write to a removed stick succeeded")
	}
}
