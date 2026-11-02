//go:build burrow

package main

import (
	"burrow/kernel/bootinfo"
	"burrow/kernel/fs/memfs"
	"burrow/kernel/fs/tarfs"
	"burrow/kernel/klog"
)

// mountInitrd indexes the "initrd" boot module in place (W8-S02). With no
// initrd (regression images) the kernel runs on with an empty tree.
func mountInitrd(mods []bootinfo.Module, log *klog.Logger) *memfs.FS {
	m, ok := bootinfo.Find(mods, "initrd")
	if !ok {
		log.Warnf("no initrd module: empty root")
		return memfs.New()
	}
	fsys, err := tarfs.New(moduleBytes(m))
	if err != nil {
		log.Errorf("%v: empty root", err)
		return memfs.New()
	}
	files, dirs, size := fsys.Counts()
	log.Infof("tarfs: %d files, %d directories, %d bytes", files, dirs, size)
	return fsys
}
