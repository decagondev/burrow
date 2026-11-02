// Package klog is the kernel logger. It depends on io.Writer only, so the
// sink can be a UART today, a framebuffer console tomorrow, or a bytes.Buffer
// in tests (Dependency Inversion + Open/Closed).
package klog

import (
	"fmt"
	"io"
	"sync"
)

type Level int

const (
	Debug Level = iota
	Info
	Warn
	Error
)

var levelNames = [...]string{"DEBUG", "INFO", "WARN", "ERROR"}

// Logger is safe for concurrent use by many goroutines.
type Logger struct {
	mu        sync.Mutex
	out       io.Writer
	min       Level
	subsystem string
}

func New(out io.Writer, min Level) *Logger { return &Logger{out: out, min: min} }

// With returns a child logger that tags lines with a subsystem name and
// shares the parent's lock and sink.
func (l *Logger) With(subsystem string) *Logger {
	return &Logger{out: lockedWriter{l}, min: l.min, subsystem: subsystem}
}

func (l *Logger) Logf(lv Level, format string, args ...any) {
	if lv < l.min {
		return
	}
	line := fmt.Sprintf(format, args...)
	tag := "kernel"
	if l.subsystem != "" {
		tag = l.subsystem
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, "[%s] %s: %s\n", tag, levelNames[lv], line)
}

func (l *Logger) Infof(f string, a ...any)  { l.Logf(Info, f, a...) }
func (l *Logger) Warnf(f string, a ...any)  { l.Logf(Warn, f, a...) }
func (l *Logger) Errorf(f string, a ...any) { l.Logf(Error, f, a...) }
func (l *Logger) Debugf(f string, a ...any) { l.Logf(Debug, f, a...) }

type lockedWriter struct{ parent *Logger }

func (w lockedWriter) Write(p []byte) (int, error) {
	w.parent.mu.Lock()
	defer w.parent.mu.Unlock()
	return w.parent.out.Write(p)
}
