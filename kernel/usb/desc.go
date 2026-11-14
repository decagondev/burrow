// desc.go parses the device and configuration descriptors (USB 2.0
// §9.6). It never trusts a length: a descriptor that runs past its buffer
// is an error, and an unknown descriptor type is skipped by its length.
package usb

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// DeviceDescriptor is the 18-byte device descriptor.
type DeviceDescriptor struct {
	USB                          uint16 // bcdUSB
	Class, SubClass, Protocol    uint8
	MaxPacket0                   uint8 // endpoint 0's packet size (a power of two, the exponent from USB 3)
	Vendor, Product, Release     uint16
	IManufacturer, IProduct, ISN uint8
	NumConfigs                   uint8
}

const deviceDescBytes = 18

var errShort = errors.New("usb: descriptor too short")

// ParseDevice reads a device descriptor (at least its first 8 bytes give
// MaxPacket0; the rest need all 18).
func ParseDevice(b []byte) (DeviceDescriptor, error) {
	if len(b) < 8 || b[1] != DescDevice {
		return DeviceDescriptor{}, fmt.Errorf("usb: not a device descriptor (% x)", b[:min(len(b), 2)])
	}
	d := DeviceDescriptor{USB: binary.LittleEndian.Uint16(b[2:]), Class: b[4], SubClass: b[5],
		Protocol: b[6], MaxPacket0: b[7]}
	if len(b) < deviceDescBytes {
		return d, errShort
	}
	d.Vendor = binary.LittleEndian.Uint16(b[8:])
	d.Product = binary.LittleEndian.Uint16(b[10:])
	d.Release = binary.LittleEndian.Uint16(b[12:])
	d.IManufacturer, d.IProduct, d.ISN, d.NumConfigs = b[14], b[15], b[16], b[17]
	return d, nil
}

// Endpoint is an endpoint descriptor.
type Endpoint struct {
	Address    uint8 // bit 7: IN
	Attributes uint8 // bits 1:0: the transfer type
	MaxPacket  uint16
	Interval   uint8
}

// Transfer types (bmAttributes bits 1:0).
const (
	XferControl   = 0
	XferIso       = 1
	XferBulk      = 2
	XferInterrupt = 3
)

func (e Endpoint) In() bool    { return e.Address&0x80 != 0 }
func (e Endpoint) Number() int { return int(e.Address & 0x0F) }
func (e Endpoint) Type() int   { return int(e.Attributes & 3) }

// Interface is one interface (alternate setting 0) and its endpoints.
type Interface struct {
	Number, Class, SubClass, Protocol uint8
	Endpoints                         []Endpoint
}

// Config is a configuration and its interfaces.
type Config struct {
	Value       uint8 // bConfigurationValue, for SET_CONFIGURATION
	TotalLength uint16
	Interfaces  []Interface
}

const configDescBytes = 9

// ParseConfig reads a configuration descriptor and what follows it. With
// only its first 9 bytes it gives TotalLength, the size to read again.
func ParseConfig(b []byte) (Config, error) {
	if len(b) < configDescBytes || b[1] != DescConfig {
		return Config{}, fmt.Errorf("usb: not a configuration descriptor")
	}
	c := Config{TotalLength: binary.LittleEndian.Uint16(b[2:]), Value: b[5]}
	b = b[:min(len(b), int(c.TotalLength))]
	var cur *Interface // alternate settings other than 0 are skipped
	for off := int(b[0]); off < len(b); {
		n := int(b[off])
		if n < 2 || off+n > len(b) {
			return c, fmt.Errorf("usb: descriptor at %d runs past %d bytes", off, len(b))
		}
		d := b[off : off+n]
		switch {
		case d[1] == DescInterface && n >= 9:
			cur = nil
			if d[3] == 0 {
				c.Interfaces = append(c.Interfaces, Interface{Number: d[2], Class: d[5], SubClass: d[6], Protocol: d[7]})
				cur = &c.Interfaces[len(c.Interfaces)-1]
			}
		case d[1] == DescEndpoint && n >= 7 && cur != nil:
			cur.Endpoints = append(cur.Endpoints, Endpoint{Address: d[2], Attributes: d[3],
				MaxPacket: binary.LittleEndian.Uint16(d[4:]) & 0x7FF, Interval: d[6]})
		}
		off += n
	}
	return c, nil
}

// ReadConfig reads configuration 0 whole: 9 bytes, then TotalLength.
func ReadConfig(d Device) (Config, error) {
	head := make([]byte, configDescBytes)
	if _, err := GetDescriptor(d, DescConfig, 0, 0, head); err != nil {
		return Config{}, err
	}
	c, err := ParseConfig(head)
	if err != nil {
		return c, err
	}
	full := make([]byte, max(int(c.TotalLength), configDescBytes))
	n, err := GetDescriptor(d, DescConfig, 0, 0, full)
	if err != nil {
		return c, err
	}
	return ParseConfig(full[:n])
}
