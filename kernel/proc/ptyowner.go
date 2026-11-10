package proc

// ptyowner.go - the owner and mode of a pseudo-terminal's slave (W24-S03):
// /dev/pts/N belongs to whoever opened /dev/ptmx, mode 0620 as Linux's
// devpts gives it, and chown and chmod change them with the usual rules
// (ownercalls.go checks them): dropbear, as root, gives a session's terminal
// to the user before it starts the shell, and fails the session if it
// cannot. Kept beside the pair in System and dropped with it; stat and
// fstat of a slave both describe it from here, so ttyname's comparison
// holds. Never a file of the tree. Docs: WAVE-24-ssh-browser.md.

import (
	"io/fs"
	"strconv"
	"strings"
	"syscall"
	"time"

	"burrow/kernel/fsattr"
)

const (
	ptsMode    fs.FileMode = 0o620
	ptsInoBase uint64      = 0x707473 << 16 // "pts": st_ino of /dev/pts/N is this plus N
)

// ptyAttr is a slave's owner and permission bits.
type ptyAttr struct {
	uid, gid uint32
	mode     fs.FileMode
}

// ptsInfo describes /dev/pts/N.
type ptsInfo struct {
	n int
	a ptyAttr
}

func (i ptsInfo) Name() string       { return strconv.Itoa(i.n) }
func (i ptsInfo) Size() int64        { return 0 }
func (i ptsInfo) Mode() fs.FileMode  { return fs.ModeDevice | fs.ModeCharDevice | i.a.mode }
func (i ptsInfo) ModTime() time.Time { return time.Time{} }
func (i ptsInfo) IsDir() bool        { return false }
func (i ptsInfo) Sys() any {
	return fsattr.Attr{UID: i.a.uid, GID: i.a.gid, Nlink: 1, Ino: ptsInoBase + uint64(i.n)}
}

// ptyNumber is N of dev/pts/N, or -1.
func ptyNumber(name string) int {
	if !strings.HasPrefix(name, ptsPrefix) {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimPrefix(name, ptsPrefix))
	if err != nil || n < 0 {
		return -1
	}
	return n
}

// ptsInfoLocked is pair n's description; s.mu is held and the pair open.
func (s *System) ptsInfoLocked(n int) ptsInfo {
	if a := s.ptyAttrs[n]; a != nil {
		return ptsInfo{n, *a}
	}
	return ptsInfo{n, ptyAttr{mode: ptsMode}}
}

// setPTY changes slave name's owner (fsattr.Keep leaves one) or, with
// chmod, its permission bits; ENOENT unless the pair is open.
func (s *System) setPTY(name string, uid, gid uint32, mode fs.FileMode, chmod bool) error {
	n := ptyNumber(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < 0 || s.ptys[n] == nil {
		return syscall.ENOENT
	}
	a := s.ptsInfoLocked(n).a
	switch {
	case chmod:
		a.mode = mode & fs.ModePerm
	default:
		if uid != fsattr.Keep {
			a.uid = uid
		}
		if gid != fsattr.Keep {
			a.gid = gid
		}
	}
	if s.ptyAttrs == nil {
		s.ptyAttrs = map[int]*ptyAttr{}
	}
	s.ptyAttrs[n] = &a
	return nil
}
