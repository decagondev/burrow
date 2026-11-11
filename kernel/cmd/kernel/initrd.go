//go:build burrow

package main

import (
	"io"
	"io/fs"

	"burrow/kernel/arch/amd64"
	"burrow/kernel/bootinfo"
	"burrow/kernel/fs/devfs"
	"burrow/kernel/fs/memfs"
	"burrow/kernel/fs/tarfs"
	"burrow/kernel/klog"
	"burrow/kernel/lattrace"
	"burrow/kernel/random"
	"burrow/kernel/vfs"
)

// mountFiles builds the file tree: the initrd at /, a ramfs at /tmp and
// devfs at /dev with the standard devices (drivers add theirs through
// driver.Env.Dev). console is where /dev/console writes go.
func (k *kernel) mountFiles(console io.Writer) *devfs.FS {
	k.fs = vfs.New()
	dev := devfs.New()
	dev.Register("null", devfs.Null{}, 0o666)
	dev.Register("zero", devfs.Zero{}, 0o666)
	dev.Register("console", devfs.WriteOnly{Writer: console}, 0o620)
	dev.Register("random", random.Kernel, 0o666) // W14-S03: not for cryptography
	dev.Register("urandom", random.Kernel, 0o666)
	dev.Register("lattrace", lattrace.New(amd64.LatTracer{}), 0o666) // W25-S01: the round trip's steps
	for _, name := range []string{"tty", "tty0", "tty1"} {           // listed here; proc opens the terminal (W14-S08)
		dev.Register(name, devfs.WriteOnly{Writer: console}, 0o620)
	}
	k.fs.Mount(".", mountInitrd(k.modules, k.log.With("initrd")), "tarfs")
	tmp := memfs.New()
	tmp.Chmod(".", fs.ModeSticky|0o777) // W19-S04: anyone may create in /tmp, only owners delete
	k.fs.Mount("tmp", tmp, "ramfs")
	k.fs.Mount("dev", dev, "devfs")
	return dev
}

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
