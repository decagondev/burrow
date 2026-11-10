package vfs

// virtual.go - directories that only mounts make (BUG-0030, W24-S02): a
// mount at opt/net on a root without opt still needs opt to be a directory
// the walker can pass and a listing can show. below says what the mounts
// under a directory add to it: each mount point directly inside (its root,
// named after the point) and, for a mount further down, the directory on
// the way to it. A directory that exists only that way opens as a
// read-only, empty one with those entries. Never invents anything where no
// mount lies below. Docs: docs/living/BUGS_AND_MITIGATIONS.md (BUG-0030).

import (
	"io/fs"
	"strings"
	"time"
)

// below is the entries the mounts under dir add to it, by name.
func (v *VFS) below(dir string) map[string]fs.FileInfo {
	v.mu.RLock()
	defer v.mu.RUnlock()
	prefix := dir + "/"
	if dir == "." {
		prefix = ""
	}
	out := map[string]fs.FileInfo{}
	for _, m := range v.mounts {
		rest, ok := strings.CutPrefix(m.point, prefix)
		if m.point == "." || !ok || rest == "" {
			continue
		}
		name, deeper, _ := strings.Cut(rest, "/")
		if deeper != "" {
			if _, seen := out[name]; !seen {
				out[name] = dirInfo(name)
			}
			continue
		}
		if st, err := fs.Stat(m.fsys, "."); err == nil {
			out[name] = namedInfo{st, name}
		}
	}
	return out
}

// dirInfo describes a directory that only mounts make.
type dirInfo string

func (d dirInfo) Name() string       { return string(d) }
func (d dirInfo) Size() int64        { return 0 }
func (d dirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (d dirInfo) ModTime() time.Time { return time.Time{} }
func (d dirInfo) IsDir() bool        { return true }
func (d dirInfo) Sys() any           { return nil }

// virtualDir is such a directory, open; mergedDir gives it its entries.
type virtualDir struct{ name string }

func (d virtualDir) Stat() (fs.FileInfo, error) { return dirInfo(d.name), nil }
func (d virtualDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: fs.ErrInvalid}
}
func (d virtualDir) Close() error { return nil }
