// irq.go - a PCI function's interrupt for its driver (W27-S01): MSI-X if
// the function has it, else MSI, else its INTx line, which the firmware
// wrote into the interrupt line register and devices may share (W22-S01).
// MSI lines come from the nucleus (driver.Env.AllocMSI), one per function,
// and are never shared. Never enables MSI before the line exists in the
// nucleus, so an early message waits there as pending. Docs: ADR-0014.
package pci

import (
	"fmt"
	"sync/atomic"
	"unsafe"

	"burrow/kernel/driver"
)

// Kind is how a function interrupts.
type Kind int

const (
	INTx Kind = iota
	MSI
	MSIX
)

// Vector is a function's interrupt once Setup chose it.
type Vector struct {
	Kind Kind
	Line int // the nucleus's line: the INTx line, or an MSI line (16 up)
}

func (v Vector) String() string {
	switch v.Kind {
	case MSI:
		return fmt.Sprintf("msi line %d", v.Line)
	case MSIX:
		return fmt.Sprintf("msi-x line %d", v.Line)
	}
	return fmt.Sprintf("irq %d", v.Line)
}

// Setup gives d its interrupt: MSI-X entry 0 (with noMSIX false), MSI, or
// INTx. An INTx line outside 1-15 is an error: the firmware routed nothing.
// The function is claimed for the driver env.Log names (claims.go).
func Setup(env *driver.Env, d Device, noMSIX bool) (Vector, error) {
	v, err := setup(env, d, noMSIX)
	if err == nil && env.Log != nil {
		ClaimFor(d, env.Log.Subsystem(), v.String())
	}
	return v, err
}

func setup(env *driver.Env, d Device, noMSIX bool) (Vector, error) {
	cfg := PortConfig{IO: env.Ports32}
	caps := Capabilities(cfg, d)
	if env.AllocMSI != nil {
		if off, ok := FindCap(caps, capMSIX); ok && !noMSIX {
			v, err := setupMSIX(env, cfg, d, off)
			if err == nil {
				return v, nil
			}
			if env.Log != nil {
				env.Log.Warnf("%02x:%02x.%x: msi-x: %v; trying the next", d.Bus, d.Dev, d.Fn, err)
			}
		}
		if off, ok := FindCap(caps, capMSI); ok {
			line, addr, data, err := env.AllocMSI()
			if err == nil {
				EnableMSI(cfg, d, off, addr, data)
				return Vector{Kind: MSI, Line: line}, nil
			}
		}
	}
	if d.IRQLine == 0 || d.IRQLine >= 16 {
		return Vector{}, fmt.Errorf("no usable interrupt line (%d)", d.IRQLine)
	}
	return Vector{Kind: INTx, Line: int(d.IRQLine)}, nil
}

func setupMSIX(env *driver.Env, cfg PortConfig, d Device, off uint8) (Vector, error) {
	bar, offset, _ := MSIXTable(cfg, d, off)
	if bar > 5 || d.MemBase64(bar) == 0 || env.MapMMIO == nil {
		return Vector{}, fmt.Errorf("table in BAR %d, not memory", bar)
	}
	pa := d.MemBase64(bar) + uint64(offset)
	lo := pa &^ 0xFFF
	mem, err := env.MapMMIO(lo, 0x1000)
	if err != nil {
		return Vector{}, err
	}
	entry := mem[pa-lo : pa-lo+msixEntryBytes]
	line, addr, data, err := env.AllocMSI()
	if err != nil {
		return Vector{}, err
	}
	w32 := func(o, v uint32) { atomic.StoreUint32((*uint32)(unsafe.Pointer(&entry[o])), v) }
	if err := EnableMSIX(cfg, d, off, w32, addr, data); err != nil {
		return Vector{}, err
	}
	return Vector{Kind: MSIX, Line: line}, nil
}

// Serve runs h on v's line: through env.Lines when there is one (shared
// INTx lines need it; an MSI line has one handler), else on the line alone.
func Serve(env *driver.Env, v Vector, name string, h driver.Handler) {
	if env.Lines != nil {
		env.Lines.Serve(v.Line, name, h)
		return
	}
	driver.Serve(env.IRQ(v.Line), func() { h() })
}
