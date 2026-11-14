// claims.go records which driver took each PCI function (W27-S05), for
// the hardware report (cmd/kernel/hwreport.go): pci.Setup claims a function
// for the driver whose logger it was given, with the interrupt it chose; a
// driver that sets up no interrupt (bga) claims its function itself. Never
// decides anything: a claim is a record, not a lock.
package pci

import "sync"

// Claim is a function's driver and how it serves it.
type Claim struct{ Driver, Detail string }

var (
	claimMu sync.Mutex
	claims  = map[[3]uint8]Claim{}
)

func key(d Device) [3]uint8 { return [3]uint8{d.Bus, d.Dev, d.Fn} }

// ClaimFor records that driver serves d (detail: its interrupt, or a role).
func ClaimFor(d Device, driver, detail string) {
	claimMu.Lock()
	defer claimMu.Unlock()
	claims[key(d)] = Claim{driver, detail}
}

// Claimed is d's claim, if a driver made one.
func Claimed(d Device) (Claim, bool) {
	claimMu.Lock()
	defer claimMu.Unlock()
	c, ok := claims[key(d)]
	return c, ok
}
