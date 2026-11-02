// Package tarfs mounts a ustar archive (the initrd) as a read-only fs.FS.
// The archive stays where the loader put it: file contents are slices of
// it, never copies. Directories missing from the archive are made up from
// the paths of the files inside them.
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
			err = fsys.MkdirAll(name, h.FileInfo().Mode(), h.ModTime)
		case tar.TypeReg:
			off := int64(len(archive)) - int64(r.Len()) // tar.Reader sits at the data
			if off+h.Size > int64(len(archive)) {
				return nil, fmt.Errorf("tarfs: %s runs past the archive", name)
			}
			if err = fsys.MkdirAll(path.Dir(name), 0o755, h.ModTime); err == nil {
				err = fsys.WriteFile(name, archive[off:off+h.Size:off+h.Size], h.FileInfo().Mode(), h.ModTime)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("tarfs: %s: %w", name, err)
		}
	}
	fsys.Freeze()
	return fsys, nil
}

// clean turns "./etc/motd" or "etc/" into "etc/motd" / "etc".
func clean(name string) string {
	if c := strings.TrimPrefix(path.Clean("/"+name), "/"); c != "" {
		return c
	}
	return "."
}
