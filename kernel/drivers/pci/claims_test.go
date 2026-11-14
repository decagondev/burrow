package pci

import (
	"io"
	"testing"

	"burrow/kernel/driver"
	"burrow/kernel/klog"
)

// TestSetupClaims: Setup records the function for the driver its logger
// names, with the interrupt it chose; a function nobody set up has no claim.
func TestSetupClaims(t *testing.T) {
	d := Device{Bus: 0, Dev: 7, Fn: 1, IRQLine: 11}
	other := Device{Bus: 0, Dev: 8}
	env := &driver.Env{Ports32: fakeNoCaps{}, Log: klog.New(io.Discard, klog.Info).With("e1000")}
	if _, err := Setup(env, d, false); err != nil {
		t.Fatal(err)
	}
	c, ok := Claimed(d)
	if !ok || c.Driver != "e1000" || c.Detail != "irq 11" {
		t.Fatalf("claim %+v %v", c, ok)
	}
	if _, ok := Claimed(other); ok {
		t.Fatal("a function nobody set up is claimed")
	}
	ClaimFor(other, "bga", "display")
	if c, _ := Claimed(other); c.Driver != "bga" {
		t.Fatalf("explicit claim %+v", c)
	}
}

// fakeNoCaps is a configuration space with no capability list.
type fakeNoCaps struct{}

func (fakeNoCaps) In32(port uint16) uint32     { return 0 }
func (fakeNoCaps) Out32(port uint16, v uint32) {}
