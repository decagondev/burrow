// bot.go runs one Bulk-Only Transport command (BOT 1.0 §5.3): the command
// block wrapper, the data stage in page-sized transfers, the status
// wrapper. A stall in the data stage ends it (the endpoint is cleared and
// the status read, §6.7.2); a stalled status read is cleared and tried
// once more; a status wrapper that is not valid and meaningful, or a phase
// error, gets the reset recovery (§5.3.4).
package usb

import (
	"encoding/binary"
	"errors"
	"fmt"
)

func (k *Disk) command(cb []byte, data []byte, in bool) error {
	k.tag++
	cbw := make([]byte, cbwBytes)
	binary.LittleEndian.PutUint32(cbw[0:], cbwSig)
	binary.LittleEndian.PutUint32(cbw[4:], k.tag)
	binary.LittleEndian.PutUint32(cbw[8:], uint32(len(data)))
	if in {
		cbw[12] = cbwFlagIn
	}
	cbw[14] = byte(len(cb))
	copy(cbw[15:], cb)
	if _, err := k.out.Transfer(cbw); err != nil {
		k.resetRecovery()
		return fmt.Errorf("command wrapper: %w", err)
	}
	if err := k.dataStage(data, in); err != nil {
		return err
	}
	csw, err := k.status()
	if err != nil {
		k.resetRecovery()
		return err
	}
	switch {
	case binary.LittleEndian.Uint32(csw[0:]) != cswSig || binary.LittleEndian.Uint32(csw[4:]) != k.tag:
		k.resetRecovery()
		return fmt.Errorf("status wrapper not valid (% x)", csw)
	case csw[12] == cswPassed:
		return nil
	case csw[12] == cswFailed:
		return errCommand
	}
	k.resetRecovery()
	return fmt.Errorf("phase error (status %d)", csw[12])
}

// dataStage moves data in transfers of up to a page; a short IN transfer
// or a stall ends it early (the status wrapper says what happened).
func (k *Disk) dataStage(data []byte, in bool) error {
	p, ep := k.out, k.outEP
	if in {
		p, ep = k.in, k.inEP
	}
	for off := 0; off < len(data); {
		chunk := data[off:min(len(data), off+xferChunkBytes)]
		n, err := p.Transfer(chunk)
		if errors.Is(err, ErrStall) {
			return k.clearHalt(ep)
		}
		if err != nil {
			return fmt.Errorf("data: %w", err)
		}
		off += n
		if n < len(chunk) {
			return nil
		}
	}
	return nil
}

func (k *Disk) status() ([]byte, error) {
	csw := make([]byte, cswBytes)
	n, err := k.in.Transfer(csw)
	if errors.Is(err, ErrStall) {
		if err := k.clearHalt(k.inEP); err != nil {
			return nil, err
		}
		n, err = k.in.Transfer(csw)
	}
	if err != nil {
		return nil, fmt.Errorf("status wrapper: %w", err)
	}
	if n != cswBytes {
		return nil, fmt.Errorf("status wrapper of %d bytes", n)
	}
	return csw, nil
}

// clearHalt clears an endpoint's halt on the device (CLEAR_FEATURE
// ENDPOINT_HALT; the host side was reset when the stall was seen).
func (k *Disk) clearHalt(ep Endpoint) error {
	_, err := k.d.Control(Setup{RequestType: recipEndpoint, Request: reqClearFeat, Value: featureHalt,
		Index: uint16(ep.Address)}, nil)
	return err
}

// resetRecovery is BOT's reset, then both endpoints' halts cleared.
func (k *Disk) resetRecovery() {
	k.d.Control(Setup{RequestType: TypeClass | RecipInterface, Request: reqBOTReset, Index: uint16(k.ifc)}, nil)
	k.clearHalt(k.inEP)
	k.clearHalt(k.outEP)
}
