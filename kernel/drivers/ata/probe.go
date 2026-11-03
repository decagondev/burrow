package ata

// probe.go - the ATA driver (W7/W8, both channels since W15-S06): IDENTIFY
// on the primary channel's master and slave (hda, hdb) and on the
// secondary channel's (hdc, hdd), each found disk registered as a
// block.Device. The ports it is given keep their string I/O (REP INSW):
// wrapped without it, BUG-0017's fix ran a word at a time.

import (
	"errors"

	"burrow/kernel/block"
	"burrow/kernel/driver"
	"burrow/kernel/hal"
)

const (
	secondaryBase uint16 = 0x170
	secondaryCtl  uint16 = 0x376
)

func init() { driver.Register(&controller{}) }

type controller struct{}

func (*controller) Name() string { return "ata" }

// Probe: a floating bus (no controller) reads 0xFF.
func (*controller) Probe(env *driver.Env) bool {
	return env.Ports16 != nil && env.Ports.In8(ioBase+regStatus) != 0xFF
}

type ports struct {
	hal.PortIO
	hal.PortIO16
}

// stringPorts are ports with string I/O, as the real ones have.
type stringPorts struct {
	ports
	hal.PortIOString
}

func (*controller) Start(env *driver.Env) error {
	var io Ports = ports{env.Ports, env.Ports16}
	if s, ok := env.Ports16.(hal.PortIOString); ok {
		io = stringPorts{ports{env.Ports, env.Ports16}, s}
	}
	found := 0
	for _, ch := range []struct {
		c     *Channel
		names [2]string
	}{
		{NewChannelAt(io, ioBase, ctlBase), [2]string{"hda", "hdb"}},
		{NewChannelAt(io, secondaryBase, secondaryCtl), [2]string{"hdc", "hdd"}},
	} {
		if env.Ports.In8(ch.c.base+regStatus) == 0xFF {
			continue // no drive on this channel: the bus floats
		}
		for i, name := range ch.names {
			d, err := ch.c.Identify(i == 1)
			if err != nil {
				continue
			}
			block.Register(name, d)
			mode := "READ SECTORS"
			if d.multi > 0 {
				mode = "READ MULTIPLE"
			}
			env.Log.Infof("%s: %q, %d sectors (%d KiB), %s", name, d.Model, d.sectors, d.sectors/2, mode)
			found++
		}
	}
	if found == 0 {
		return errors.New("no ATA disks")
	}
	return nil
}
