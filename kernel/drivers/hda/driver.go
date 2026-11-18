// driver.go - the hdaudio driver's probe and start: the first PCI function of
// class 04.03 (an HD Audio controller), its registers mapped from BAR 0,
// MSI where it has it (else its INTx line, maybe shared), the controller
// reset, every codec read until one has an output path, the stream set up
// on it, and /dev/dsp registered (/dev/dsp1 when an AC'97 has dsp). The
// codec's id and the path go into the log and the hardware report's claim
// (PIT-108), so a real machine's report says which pin plays.
package hda

import (
	"errors"
	"fmt"
	"strings"

	"burrow/kernel/driver"
	"burrow/kernel/drivers/pci"
	"burrow/kernel/sound"
)

const (
	classMultimedia = 0x04
	subclassHDA     = 0x03
	mmioBytes       = 0x4000 // BAR 0: 16 KiB of registers
)

func init() { driver.Register(&hdaDriver{}) }

type hdaDriver struct{ dev pci.Device }

func (*hdaDriver) Name() string { return "hdaudio" } // not "hda", an IDE disk's name

func (h *hdaDriver) Probe(env *driver.Env) bool {
	if env.Ports32 == nil || env.MapMMIO == nil || env.DMAPage == nil || env.IRQ == nil || env.Dev == nil ||
		!pci.Present(env.Ports32) {
		return false
	}
	for _, d := range pci.Scan(env.Ports32) {
		if d.Class == classMultimedia && d.Subclass == subclassHDA {
			h.dev = d
			return true
		}
	}
	return false
}

func (h *hdaDriver) Start(env *driver.Env) error {
	base := h.dev.MemBase64(0)
	if base == 0 {
		return errors.New("BAR 0 is not memory")
	}
	vec, err := pci.Setup(env, h.dev, false)
	if err != nil {
		return err
	}
	pci.EnableBusMaster(pci.PortConfig{IO: env.Ports32}, h.dev)
	mem, err := env.MapMMIO(base, mmioBytes)
	if err != nil {
		return fmt.Errorf("map BAR 0 at %#x: %w", base, err)
	}
	c, err := NewController(MMIO(mem), DMAPage(env.DMAPage))
	if err != nil {
		return err
	}
	codec, path, err := choose(c)
	if err != nil {
		return err
	}
	s, err := NewStream(c, codec, path, DMAPage(env.DMAPage))
	if err != nil {
		return err
	}
	handler := s.Interrupt
	if vec.Kind != pci.INTx {
		handler = func() bool { s.Interrupt(); return true } // a message is this controller's alone
	}
	pci.Serve(env, vec, "hdaudio", handler)
	name := "dsp"
	if err := env.Dev.Register(name, sound.NewDSP(s), 0o666); err != nil {
		name = "dsp1"
		if err := env.Dev.Register(name, sound.NewDSP(s), 0o666); err != nil {
			return err
		}
	}
	detail := fmt.Sprintf("codec %d %08x, %s, %v", codec.CAd, codec.Vendor, codec.String(path), vec)
	pci.ClaimFor(h.dev, "hdaudio", detail)
	env.Log.Infof("/dev/%s: %s, %d Hz", name, detail, s.Rate())
	return nil
}

// choose reads every codec and takes the first with an output path.
func choose(c *Controller) (*Codec, Path, error) {
	var why []string
	for _, cad := range c.Codecs() {
		codec, err := ReadCodec(c, cad)
		if err == nil {
			var p Path
			if p, err = codec.OutputPath(); err == nil {
				return codec, p, nil
			}
		}
		why = append(why, fmt.Sprintf("codec %d: %v", cad, err))
	}
	return nil, Path{}, fmt.Errorf("hda: no codec with an output (%s)", strings.Join(why, "; "))
}
