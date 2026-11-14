// hba.go - the host bus adapter: AHCI mode on, every implemented port with
// an ATA disk taken over (port.go), interrupts on. Interrupt is the line's
// handler; it never blocks and claims only what the HBA raised.
package ahci

import "fmt"

// HBA is a set-up AHCI controller and its disks' ports.
type HBA struct {
	r     Regs
	Ports []*Port
	// Wide: the HBA reaches memory above 4 GiB (CAP.S64A); without it the
	// driver's DMA pages must lie below (they do: the nucleus's frames).
	Wide bool
}

// New sets the HBA up: AHCI mode, the ports, then its interrupt enabled.
// A port that fails to start is reported in errs and left alone.
func New(r Regs, dma DMAPage) (h *HBA, errs []error) {
	h = &HBA{r: r, Wide: r.R32(hbaCAP)&capS64A != 0}
	r.W32(hbaGHC, r.R32(hbaGHC)|ghcAE)
	pi := r.R32(hbaPI)
	for n := 0; n < maxPorts; n++ {
		if pi&(1<<n) == 0 {
			continue
		}
		p, err := newPort(r, n, dma)
		if err != nil {
			errs = append(errs, fmt.Errorf("port %d: %w", n, err))
			continue
		}
		if p != nil {
			h.Ports = append(h.Ports, p)
		}
	}
	r.W32(hbaIS, r.R32(hbaIS))
	r.W32(hbaGHC, r.R32(hbaGHC)|ghcIE)
	return h, errs
}

// Identify asks each port's disk for its size and model; a disk that does
// not answer is dropped from Ports. Call it once the interrupt is served.
func (h *HBA) Identify() (errs []error) {
	var ok []*Port
	for _, p := range h.Ports {
		if err := p.identify(); err != nil {
			errs = append(errs, fmt.Errorf("port %d: identify: %w", p.Number, err))
			continue
		}
		ok = append(ok, p)
	}
	h.Ports = ok
	return errs
}

// Interrupt serves every port the HBA says raised it; false: not this HBA's.
func (h *HBA) Interrupt() bool {
	is := h.r.R32(hbaIS)
	if is == 0 {
		return false
	}
	for _, p := range h.Ports {
		if is&(1<<p.Number) != 0 {
			p.interrupt()
		}
	}
	h.r.W32(hbaIS, is)
	return true
}
