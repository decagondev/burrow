// device.go - the common configuration (§4.1.4.3): reset, the status
// handshake, feature negotiation, a queue's addresses, notification and the
// ISR byte. Never accepts a feature it was not asked for; never runs on a
// device without VIRTIO_F_VERSION_1.
package virtio

import (
	"errors"
	"sync/atomic"
	"unsafe"
)

// Regs is one configuration structure's registers. MMIO is the real thing;
// tests give a fake device's.
type Regs interface {
	R8(off uint32) uint8
	R16(off uint32) uint16
	R32(off uint32) uint32
	W8(off uint32, v uint8)
	W16(off uint32, v uint16)
	W32(off uint32, v uint32)
}

// MMIO is mapped device memory. Each access is one load or store of its width.
type MMIO []byte

//go:noinline
func (m MMIO) R8(off uint32) uint8 { return *(*uint8)(unsafe.Pointer(&m[off])) }

//go:noinline
func (m MMIO) R16(off uint32) uint16 { return *(*uint16)(unsafe.Pointer(&m[off])) }

func (m MMIO) R32(off uint32) uint32 { return atomic.LoadUint32((*uint32)(unsafe.Pointer(&m[off]))) }

//go:noinline
func (m MMIO) W8(off uint32, v uint8) { *(*uint8)(unsafe.Pointer(&m[off])) = v }

//go:noinline
func (m MMIO) W16(off uint32, v uint16) { *(*uint16)(unsafe.Pointer(&m[off])) = v }

func (m MMIO) W32(off uint32, v uint32) { atomic.StoreUint32((*uint32)(unsafe.Pointer(&m[off])), v) }

// The common configuration structure (virtio_pci_common_cfg).
const (
	ccDeviceFeatureSel = 0x00
	ccDeviceFeature    = 0x04
	ccDriverFeatureSel = 0x08
	ccDriverFeature    = 0x0C
	ccStatus           = 0x14
	ccQueueSelect      = 0x16
	ccQueueSize        = 0x18
	ccQueueEnable      = 0x1C
	ccQueueNotifyOff   = 0x1E
	ccQueueDesc        = 0x20
	ccQueueDriver      = 0x28
	ccQueueDevice      = 0x30

	StatusAcknowledge = 1
	StatusDriver      = 2
	StatusDriverOK    = 4
	StatusFeaturesOK  = 8
	StatusFailed      = 128

	FeatureVersion1 = 1 << 32

	ISRQueue  = 1 << 0
	ISRConfig = 1 << 1

	resetPolls = 1000
)

// Device is a virtio device's transport.
type Device struct {
	Common, Notify, ISR, Config Regs
	NotifyMultiplier            uint32
}

func (d *Device) status() uint8 { return d.Common.R8(ccStatus) }

func (d *Device) addStatus(s uint8) { d.Common.W8(ccStatus, d.status()|s) }

// Negotiate resets the device and agrees on features: VIRTIO_F_VERSION_1 and
// whichever of want the device offers. It returns the features agreed.
func (d *Device) Negotiate(want uint64) (uint64, error) {
	d.Common.W8(ccStatus, 0)
	for i := 0; d.status() != 0; i++ {
		if i == resetPolls {
			return 0, errors.New("virtio: the device does not reset")
		}
	}
	d.addStatus(StatusAcknowledge)
	d.addStatus(StatusDriver)
	d.Common.W32(ccDeviceFeatureSel, 0)
	offered := uint64(d.Common.R32(ccDeviceFeature))
	d.Common.W32(ccDeviceFeatureSel, 1)
	offered |= uint64(d.Common.R32(ccDeviceFeature)) << 32
	if offered&FeatureVersion1 == 0 {
		d.addStatus(StatusFailed)
		return 0, errors.New("virtio: the device does not offer VIRTIO_F_VERSION_1 (legacy only)")
	}
	agreed := offered & (want | FeatureVersion1)
	d.Common.W32(ccDriverFeatureSel, 0)
	d.Common.W32(ccDriverFeature, uint32(agreed))
	d.Common.W32(ccDriverFeatureSel, 1)
	d.Common.W32(ccDriverFeature, uint32(agreed>>32))
	d.addStatus(StatusFeaturesOK)
	if d.status()&StatusFeaturesOK == 0 {
		d.addStatus(StatusFailed)
		return 0, errors.New("virtio: the device refused the features")
	}
	return agreed, nil
}

// QueueMax is the largest size the device allows for queue idx (0: none).
func (d *Device) QueueMax(idx uint16) uint16 {
	d.Common.W16(ccQueueSelect, idx)
	return d.Common.R16(ccQueueSize)
}

func (d *Device) w64(off uint32, v uint64) {
	d.Common.W32(off, uint32(v))
	d.Common.W32(off+4, uint32(v>>32))
}

// SetupQueue gives the device q's rings and enables it.
func (d *Device) SetupQueue(q *Queue) error {
	if max := d.QueueMax(q.Index); max == 0 || q.Size > max {
		return errors.New("virtio: the device has no such queue, or a smaller one")
	}
	d.Common.W16(ccQueueSize, q.Size)
	d.w64(ccQueueDesc, q.DescPA())
	d.w64(ccQueueDriver, q.AvailPA())
	d.w64(ccQueueDevice, q.UsedPA())
	q.notifyOff = uint32(d.Common.R16(ccQueueNotifyOff)) * d.NotifyMultiplier
	d.Common.W16(ccQueueEnable, 1)
	return nil
}

// Ready tells the device the driver is set up (DRIVER_OK).
func (d *Device) Ready() { d.addStatus(StatusDriverOK) }

// Kick tells the device that q has new available buffers.
func (d *Device) Kick(q *Queue) { d.Notify.W16(q.notifyOff, q.Index) }

// ReadISR returns and clears the interrupt status (ISRQueue, ISRConfig):
// 0 means the interrupt on a shared line was another device's.
func (d *Device) ReadISR() uint8 { return d.ISR.R8(0) }
