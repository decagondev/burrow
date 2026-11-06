package devfs

// sockets.go - socket nodes in /dev (W23-S06): a program that binds a Unix
// socket at a /dev path (syslogd's /dev/log) leaves a socket node here, as
// on Linux; Remove takes such a node away again (syslogd unlinks it first).
// Devices are never removed, and a socket node is never opened: a socket
// is reached with connect or sendto. Docs: WAVE-23-proc-virtio.md.

import (
	"io/fs"
	"path"
	"time"
)

// MakeSocket makes the socket node name (vfs.VFS.MakeSocket).
func (f *FS) MakeSocket(name string, _ time.Time) error {
	if !fs.ValidPath(name) || name == "." {
		return &fs.PathError{Op: "bind", Path: name, Err: fs.ErrInvalid}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, dup := f.devs[name]; dup || f.isDir(name) {
		return &fs.PathError{Op: "bind", Path: name, Err: fs.ErrExist}
	}
	f.devs[name] = entry{mode: fs.ModeSocket | 0o777}
	return nil
}

// removeSocket takes away a socket node; anything else is not removed.
func (f *FS) removeSocket(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.devs[name]
	switch {
	case !ok:
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrNotExist}
	case e.mode.Type() != fs.ModeSocket:
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrPermission}
	}
	delete(f.devs, name)
	return nil
}

// Stat describes name without opening it (a socket node cannot be opened).
func (f *FS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	if e, ok := f.devs[name]; ok {
		return info{path.Base(name), e.mode}, nil
	}
	if f.isDir(name) {
		return info{path.Base(name), fs.ModeDir | 0o755}, nil
	}
	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}
