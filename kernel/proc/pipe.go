package proc

import (
	"encoding/binary"
	"io"
	"io/fs"
	"sync"
	"syscall"
	"time"
)

// PipeSize is how much a pipe holds before a writer blocks (Linux: 64 KiB).
const PipeSize = 64 << 10

// pipe is the buffer both ends share. A read waits for data (end of file
// once no writer is left); a write waits for room (EPIPE once no reader is
// left). Waiting can be cancelled (a signal: EINTR, W10-S04).
type pipe struct {
	mu               sync.Mutex
	buf              []byte
	readers, writers int
	changed          chan struct{} // closed and replaced on every change
}

func newPipe() (r, w *pipeEnd) {
	p := &pipe{readers: 1, writers: 1, changed: make(chan struct{})}
	return &pipeEnd{p: p, reader: true}, &pipeEnd{p: p}
}

// notify wakes every waiter (p.mu held).
func (p *pipe) notify() {
	close(p.changed)
	p.changed = make(chan struct{})
}

// pipeEnd is one end, an fs.File so a descriptor can hold it.
type pipeEnd struct {
	p      *pipe
	reader bool
	once   sync.Once
}

// Canceller is a blocking file a signal can interrupt.
type Canceller interface {
	ReadCancel(b []byte, cancel <-chan struct{}) (int, error)
	WriteCancel(b []byte, cancel <-chan struct{}) (int, error)
}

func (e *pipeEnd) Read(b []byte) (int, error)  { return e.ReadCancel(b, nil) }
func (e *pipeEnd) Write(b []byte) (int, error) { return e.WriteCancel(b, nil) }

func (e *pipeEnd) ReadCancel(b []byte, cancel <-chan struct{}) (int, error) {
	if !e.reader {
		return 0, syscall.EBADF
	}
	p := e.p
	for {
		p.mu.Lock()
		if len(p.buf) > 0 || p.writers == 0 || len(b) == 0 {
			n := copy(b, p.buf)
			p.buf = p.buf[n:]
			if n > 0 {
				p.notify()
			}
			p.mu.Unlock()
			if n == 0 && len(b) > 0 {
				return 0, io.EOF
			}
			return n, nil
		}
		ch := p.changed
		p.mu.Unlock()
		select {
		case <-ch:
		case <-cancel:
			return 0, syscall.EINTR
		}
	}
}

// WriteCancel writes everything, waiting for room as needed; a cancel
// after part of it was written reports that part (as Linux does).
func (e *pipeEnd) WriteCancel(b []byte, cancel <-chan struct{}) (int, error) {
	if e.reader {
		return 0, syscall.EBADF
	}
	p := e.p
	done := 0
	for done < len(b) {
		p.mu.Lock()
		if p.readers == 0 {
			p.mu.Unlock()
			if done > 0 {
				return done, nil
			}
			return 0, syscall.EPIPE
		}
		if room := PipeSize - len(p.buf); room > 0 {
			n := min(room, len(b)-done)
			p.buf = append(p.buf, b[done:done+n]...)
			done += n
			p.notify()
			p.mu.Unlock()
			continue
		}
		ch := p.changed
		p.mu.Unlock()
		select {
		case <-ch:
		case <-cancel:
			if done > 0 {
				return done, nil
			}
			return 0, syscall.EINTR
		}
	}
	return done, nil
}

func (e *pipeEnd) Close() error {
	e.once.Do(func() {
		p := e.p
		p.mu.Lock()
		if e.reader {
			p.readers--
		} else {
			p.writers--
		}
		p.notify()
		p.mu.Unlock()
	})
	return nil
}

// Poll: a read end is readable with data and hung up without writers; a
// write end is writable with room and in error without readers (Linux).
func (e *pipeEnd) Poll() (int16, <-chan struct{}) {
	p := e.p
	p.mu.Lock()
	defer p.mu.Unlock()
	var rev int16
	switch {
	case e.reader && len(p.buf) > 0:
		rev = pollIn
	case !e.reader && p.readers == 0:
		rev = pollErr
	case !e.reader && len(p.buf) < PipeSize:
		rev = pollOut
	}
	if e.reader && p.writers == 0 {
		rev |= pollHup
	}
	return rev, p.changed
}

// Ioctl: FIONREAD is the bytes buffered (BUG-0016: Qt's QProcess reads
// that many from a child's pipe, and read one byte at a time without it).
func (e *pipeEnd) Ioctl(req, arg uint64, _, out func(va uint64, b []byte) error) (int64, error) {
	if req != fionread {
		return 0, syscall.ENOTTY
	}
	e.p.mu.Lock()
	n := len(e.p.buf)
	e.p.mu.Unlock()
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(n))
	return 0, out(arg, b[:])
}

func (e *pipeEnd) Stat() (fs.FileInfo, error) { return pipeInfo{}, nil }

type pipeInfo struct{}

func (pipeInfo) Name() string       { return "pipe" }
func (pipeInfo) Size() int64        { return 0 }
func (pipeInfo) Mode() fs.FileMode  { return fs.ModeNamedPipe | 0o600 }
func (pipeInfo) ModTime() time.Time { return time.Time{} }
func (pipeInfo) IsDir() bool        { return false }
func (pipeInfo) Sys() any           { return nil }
