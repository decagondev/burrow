// Package usb is the USB stack above a host controller (W27-S03): the
// standard requests, descriptors, and the class drivers a device is bound
// to (HID boot keyboards and mice here; mass storage in S04). A host
// controller driver (drivers/xhci) enumerates a device, gives it an
// address, and hands it over as a Device: control transfers on endpoint 0
// and Pipes for its other endpoints. This package never touches a
// controller's registers or DMA memory: every buffer it passes is plain Go
// memory the controller driver copies. USB 2.0 §9, HID 1.11 (boot
// protocol, appendix B). Docs: docs/04-plan/waves/WAVE-27-real-pc.md (S03),
// ADR-0014.
package usb

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Setup is a control transfer's setup packet (USB 2.0 §9.3).
type Setup struct {
	RequestType, Request uint8
	Value, Index, Length uint16
}

// Bytes is the packet as the wire carries it.
func (s Setup) Bytes() [8]byte {
	var b [8]byte
	b[0], b[1] = s.RequestType, s.Request
	binary.LittleEndian.PutUint16(b[2:], s.Value)
	binary.LittleEndian.PutUint16(b[4:], s.Index)
	binary.LittleEndian.PutUint16(b[6:], s.Length)
	return b
}

// In says the data stage, if any, is device to host.
func (s Setup) In() bool { return s.RequestType&DirIn != 0 }

// Request types, requests and descriptor types (USB 2.0 §9.4; HID 1.11 §7.2).
const (
	DirIn          = 0x80
	TypeClass      = 0x20
	RecipInterface = 0x01

	ReqGetDescriptor    = 0x06
	ReqSetConfiguration = 0x09
	ReqSetIdle          = 0x0A
	ReqSetProtocol      = 0x0B

	DescDevice    = 1
	DescConfig    = 2
	DescString    = 3
	DescInterface = 4
	DescEndpoint  = 5

	langEnglishUS = 0x0409
)

// Speed is a device's speed, numbered as xHCI's port speed IDs are.
type Speed int

const (
	SpeedFull  Speed = 1
	SpeedLow   Speed = 2
	SpeedHigh  Speed = 3
	SpeedSuper Speed = 4
)

func (s Speed) String() string {
	switch s {
	case SpeedFull:
		return "full-speed"
	case SpeedLow:
		return "low-speed"
	case SpeedHigh:
		return "high-speed"
	case SpeedSuper:
		return "super-speed"
	}
	return fmt.Sprintf("speed %d", int(s))
}

// Device is an addressed device, as its host controller driver serves it.
type Device interface {
	// Control runs a control transfer on endpoint 0; data is the data
	// stage (len(data) == s.Length). It returns the bytes moved.
	Control(s Setup, data []byte) (int, error)
	// Open configures one of the device's endpoints (after
	// SET_CONFIGURATION) and returns its pipe.
	Open(ep Endpoint) (Pipe, error)
}

// Pipe is an interrupt or bulk endpoint: one transfer at a time.
type Pipe interface {
	// Transfer moves buf (IN: fills it; OUT: sends it) and returns the
	// bytes moved; an IN transfer may end short. It blocks until done.
	Transfer(buf []byte) (int, error)
}

// ErrStall is an endpoint's STALL handshake.
var ErrStall = errors.New("usb: stall")

// GetDescriptor reads a descriptor into buf.
func GetDescriptor(d Device, typ, index uint8, lang uint16, buf []byte) (int, error) {
	return d.Control(Setup{RequestType: DirIn, Request: ReqGetDescriptor,
		Value: uint16(typ)<<8 | uint16(index), Index: lang, Length: uint16(len(buf))}, buf)
}

// SetConfiguration selects a configuration by its value.
func SetConfiguration(d Device, value uint8) error {
	_, err := d.Control(Setup{Request: ReqSetConfiguration, Value: uint16(value)}, nil)
	return err
}

// ReadString is string descriptor idx in US English, "" when there is
// none or it cannot be read.
func ReadString(d Device, idx uint8) string {
	if idx == 0 {
		return ""
	}
	buf := make([]byte, 255)
	n, err := GetDescriptor(d, DescString, idx, langEnglishUS, buf)
	if err != nil || n < 2 || buf[1] != DescString {
		return ""
	}
	n = min(n, int(buf[0]))
	var out []rune
	for i := 2; i+1 < n; i += 2 {
		out = append(out, rune(binary.LittleEndian.Uint16(buf[i:])))
	}
	return string(out)
}
