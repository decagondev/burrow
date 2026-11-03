//go:build burrow

package main

import (
	"image"
	"io"
	"sync"
	"time"

	"burrow/kernel/drivers/bga"
	"burrow/kernel/evdev"
	"burrow/kernel/fbdev"
	"burrow/kernel/fs/devfs"
	"burrow/kernel/gfx"
)

// Screen layout (tests/smoke/expect/boot_to_go.screen checks pixels of it):
// a band in the gopher's blues with the title, the text console below.
const (
	bandHeight  = 96
	titleScale  = 6
	consoleText = 2
)

var (
	bandLeft  = gfx.RGB(0x00, 0x7D, 0x9C)
	bandRight = gfx.RGB(0x5D, 0xC9, 0xE2)
	titleInk  = gfx.RGB(0xFF, 0xFF, 0xFF)
	textInk   = gfx.RGB(0xE6, 0xE6, 0xE6)
	textPaper = gfx.RGB(0x1E, 0x1E, 0x28)
)

// consoleOut is where console text goes: serial from the start, the screen
// once its driver is up. /dev/console writes here too.
type consoleOut struct {
	mu sync.Mutex
	ws []io.Writer
}

func (c *consoleOut) Add(w io.Writer) { c.mu.Lock(); c.ws = append(c.ws, w); c.mu.Unlock() }

func (c *consoleOut) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, w := range c.ws {
		w.Write(p)
	}
	return len(p), nil
}

// startScreen draws the splash and returns the on-screen console, or nil
// when there is no framebuffer (the console then stays serial-only).
func startScreen() *gfx.Console {
	fb := bga.Screen()
	if fb == nil {
		return nil
	}
	band := image.Rect(0, 0, fb.Width, bandHeight)
	fb.Gradient(band, bandLeft, bandRight)
	fb.DrawText(24, 18, "Burrow", titleInk, titleScale)
	fb.DrawText(24+7*gfx.CellW*titleScale, 18+(gfx.GlyphH-1)*titleScale-gfx.GlyphH*2,
		"files and pixels from Go", titleInk, 2)
	return gfx.NewConsole(fb, image.Rect(0, bandHeight, fb.Width, fb.Height), consoleText, textInk, textPaper)
}

// screenOut is the on-screen console. It stands aside while a program
// holds /dev/fb0 (W11-S01) and, when the last one lets go, repaints the
// splash and starts a fresh console (the program's pixels are gone).
type screenOut struct {
	mu   sync.Mutex
	con  *gfx.Console
	held bool
}

func (s *screenOut) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held {
		return len(p), nil // the program owns the pixels; serial still has the text
	}
	return s.con.Write(p)
}

func (s *screenOut) hold(held bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held = held
	if !held {
		s.con = startScreen()
	}
}

// registerFB0 makes the framebuffer /dev/fb0.
func registerFB0(dev *devfs.FS, s *screenOut) error {
	fb := bga.Screen()
	base, pitch := bga.Phys()
	mode := fbdev.Mode{Phys: base, Width: fb.Width, Height: fb.Height, PitchBytes: pitch}
	return dev.Register("fb0", fbdev.New(mode, s.hold), 0o660)
}

// registerEvent0 makes the keyboard /dev/input/event0 and returns the hook
// the PS/2 driver feeds raw scancodes to (driver.Env.Scancodes).
func registerEvent0(dev *devfs.FS, boot time.Time) func(byte) bool {
	ev := evdev.New(func() time.Duration { return time.Since(boot) })
	dev.Register("input/event0", ev, 0o660)
	return ev.Feed
}
