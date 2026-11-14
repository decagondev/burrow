// storage.go is USB mass storage (W27-S04): the Bulk-Only Transport (a
// 31-byte command block wrapper out, the data, a 13-byte status wrapper
// in) carrying SCSI commands, served as a block.Writer named uba, ubb, ...
// A stalled endpoint is cleared and the status still read; a wrapper that
// makes no sense gets the reset recovery (BOT reset, then both endpoints
// cleared); a device that is gone fails each call with an error, never a
// panic. Only LUN 0, and READ(10)/WRITE(10): disks up to 2 TiB.
// USB Mass Storage Class Bulk-Only Transport 1.0; SCSI Block Commands-3.
package usb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"burrow/kernel/block"
)

// Interface codes (MSC overview 1.4 §2, §3).
const (
	ClassStorage = 0x08
	subclassSCSI = 0x06
	protocolBOT  = 0x50
)

// Wrappers and requests (BOT 1.0 §3, §5).
const (
	cbwSig        = 0x43425355 // "USBC"
	cswSig        = 0x53425355 // "USBS"
	cbwBytes      = 31
	cswBytes      = 13
	cbwFlagIn     = 0x80
	cswPassed     = 0
	cswFailed     = 1
	reqBOTReset   = 0xFF
	reqClearFeat  = 0x01
	recipEndpoint = 0x02
	featureHalt   = 0
)

// SCSI operation codes (SPC-4, SBC-3).
const (
	scsiTestUnitReady = 0x00
	scsiRequestSense  = 0x03
	scsiInquiry       = 0x12
	scsiReadCapacity  = 0x25
	scsiRead10        = 0x28
	scsiWrite10       = 0x2A
	scsiSyncCache     = 0x35
)

const (
	xferChunkBytes = 4096     // one pipe transfer: a multiple of every bulk packet size
	cmdMaxBytes    = 64 << 10 // one READ/WRITE command
	readyWait      = 5 * time.Second
)

// Disk is a mass storage device's LUN 0.
type Disk struct {
	mu        sync.Mutex
	d         Device
	ifc       uint8
	in, out   Pipe
	inEP      Endpoint
	outEP     Endpoint
	tag       uint32
	blockSize int
	blocks    uint64
	Model     string // INQUIRY's vendor and product
}

// errCommand is a command the device answered with "failed".
var errCommand = errors.New("usb storage: command failed")

func bulkEndpoints(ifc Interface) (in, out Endpoint, ok bool) {
	var haveIn, haveOut bool
	for _, ep := range ifc.Endpoints {
		if ep.Type() != XferBulk {
			continue
		}
		if ep.In() && !haveIn {
			in, haveIn = ep, true
		} else if !ep.In() && !haveOut {
			out, haveOut = ep, true
		}
	}
	return in, out, haveIn && haveOut
}

// OpenStorage opens a SCSI bulk-only interface and reads its size.
func OpenStorage(d Device, ifc Interface) (*Disk, error) {
	inEP, outEP, ok := bulkEndpoints(ifc)
	if !ok {
		return nil, fmt.Errorf("no bulk IN and OUT endpoints")
	}
	k := &Disk{d: d, ifc: ifc.Number, inEP: inEP, outEP: outEP}
	var err error
	if k.in, err = d.Open(inEP); err != nil {
		return nil, err
	}
	if k.out, err = d.Open(outEP); err != nil {
		return nil, err
	}
	inq := make([]byte, 36)
	if err := k.command([]byte{scsiInquiry, 0, 0, 0, 36, 0}, inq, true); err != nil {
		return nil, fmt.Errorf("inquiry: %w", err)
	}
	k.Model = strings.Join(strings.Fields(string(inq[8:16])+" "+string(inq[16:32])), " ")
	if err := k.waitReady(); err != nil {
		return nil, err
	}
	capb := make([]byte, 8)
	if err := k.command([]byte{scsiReadCapacity, 0, 0, 0, 0, 0, 0, 0, 0, 0}, capb, true); err != nil {
		return nil, fmt.Errorf("read capacity: %w", err)
	}
	k.blocks = uint64(binary.BigEndian.Uint32(capb[0:])) + 1
	k.blockSize = int(binary.BigEndian.Uint32(capb[4:]))
	if k.blockSize < 512 || k.blockSize > 4096 || k.blockSize&(k.blockSize-1) != 0 {
		return nil, fmt.Errorf("block size %d", k.blockSize)
	}
	return k, nil
}

// waitReady repeats TEST UNIT READY (with REQUEST SENSE after each "not
// yet", which clears it) while a device spins up or settles.
func (k *Disk) waitReady() error {
	end := time.Now().Add(readyWait)
	for {
		err := k.command(make([]byte, 6), nil, false)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errCommand) || time.Now().After(end) {
			return fmt.Errorf("not ready: %w", err)
		}
		k.command([]byte{scsiRequestSense, 0, 0, 0, 18, 0}, make([]byte, 18), true)
		time.Sleep(50 * time.Millisecond)
	}
}

func (k *Disk) SectorSize() int { return k.blockSize }
func (k *Disk) Sectors() uint64 { return k.blocks }
func (k *Disk) Flush() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	err := k.command([]byte{scsiSyncCache, 0, 0, 0, 0, 0, 0, 0, 0, 0}, nil, false)
	if errors.Is(err, errCommand) {
		return nil // a stick with no cache to flush may refuse the command
	}
	return err
}

func (k *Disk) ReadSectors(lba uint64, buf []byte) error {
	return k.rw(scsiRead10, lba, buf)
}

func (k *Disk) WriteSectors(lba uint64, buf []byte) error {
	return k.rw(scsiWrite10, lba, buf)
}

// rw reads or writes buf in commands of up to 64 KiB.
func (k *Disk) rw(op byte, lba uint64, buf []byte) error {
	if err := block.CheckRange(k, lba, buf); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	for len(buf) > 0 {
		n := min(len(buf), cmdMaxBytes)
		count := n / k.blockSize
		cb := []byte{op, 0, 0, 0, 0, 0, 0, byte(count >> 8), byte(count), 0}
		binary.BigEndian.PutUint32(cb[2:], uint32(lba))
		if err := k.command(cb, buf[:n], op == scsiRead10); err != nil {
			return fmt.Errorf("usb storage: block %d: %w", lba, err)
		}
		lba += uint64(count)
		buf = buf[n:]
	}
	return nil
}
