//go:build burrow

package main

import (
	"fmt"

	"burrow/kernel/arch/amd64"
	"burrow/kernel/bootinfo"
	"burrow/kernel/klog"
)

// bootModules reads boot_info and the loader's module table through the
// nucleus (nu_bootinfo, then the physmap) and logs each module.
func bootModules(log *klog.Logger) ([]bootinfo.Module, error) {
	info, err := bootinfo.Parse(amd64.BootInfo())
	if err != nil {
		return nil, err
	}
	mods, err := bootinfo.ParseModules(
		amd64.PhysBytes(info.ModulesAddr, uint64(info.ModuleCount*bootinfo.ModuleBytes)), info.ModuleCount)
	if err != nil {
		return nil, err
	}
	for _, m := range mods {
		log.Infof("module %s: %d bytes at %#x", m.Name, m.Size, m.Phys)
	}
	return mods, nil
}

// moduleBytes is a module's contents, in place (no copy).
func moduleBytes(m bootinfo.Module) []byte { return amd64.PhysBytes(m.Phys, m.Size) }

func listModules(mods []bootinfo.Module) string {
	s := ""
	for _, m := range mods {
		s += fmt.Sprintln(m)
	}
	return s
}
