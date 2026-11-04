// Package tarfs mounts a ustar archive (the initrd) as a read-only fs.FS.
// The archive stays where the loader put it: file contents are slices of
// it, never copies. Directories missing from the archive are made up from
// the paths of the files inside them (root's, 0755). Since W19-S04 files
// and directories keep the owners and modes (set-id bits too) of their
// headers.
package tarfs

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"burrow/kernel/fs/memfs"
	"burrow/kernel/fsattr"
)

// New indexes archive. Links and device nodes are skipped (devfs makes
// devices; links can come with a VFS that has them).
func New(archive []byte) (*memfs.FS, error) {
	fsys := memfs.New()
	r := bytes.NewReader(archive)
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tarfs: %w", err)
		}
		name := clean(h.Name)
		if name == "." {
			continue
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err = fsys.MkdirAll(name, 0o755, h.ModTime); err == nil {
				err = owned(fsys, name, h)
			}
		case tar.TypeReg:
			off := int64(len(archive)) - int64(r.Len()) // tar.Reader sits at the data
			if off+h.Size > int64(len(archive)) {
				return nil, fmt.Errorf("tarfs: %s runs past the archive", name)
			}
			if err = fsys.MkdirAll(path.Dir(name), 0o755, h.ModTime); err == nil {
				err = fsys.WriteFileAs(name, archive[off:off+h.Size:off+h.Size], attrs(h))
			}
		}
		if err != nil {
			return nil, fmt.Errorf("tarfs: %s: %w", name, err)
		}
	}
	fsys.Freeze()
	return fsys, nil
}

// attrs is what a header says a file is created with.
func attrs(h *tar.Header) fsattr.New {
	return fsattr.New{UID: uint32(h.Uid), GID: uint32(h.Gid), Mode: h.FileInfo().Mode(), Mtime: h.ModTime}
}

// owned gives a directory its header's owner and mode (it may have been
// made up already, from a file inside it that came first).
func owned(fsys *memfs.FS, name string, h *tar.Header) error {
	a := attrs(h)
	if err := fsys.Chown(name, a.UID, a.GID); err != nil {
		return err
	}
	return fsys.Chmod(name, a.Mode)
}

// clean turns "./etc/motd" or "etc/" into "etc/motd" / "etc".
func clean(name string) string {
	if c := strings.TrimPrefix(path.Clean("/"+name), "/"); c != "" {
		return c
	}
	return "."
}
