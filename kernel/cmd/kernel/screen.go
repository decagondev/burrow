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
// holds /dev/fb0 (W11-S01) or has set KD_GRAPHICS (W14-S08), leaving it a
// black screen, and, when both let go, repaints the splash and starts a
// fresh console.
type screenOut struct {
	mu           sync.Mutex
	con          *gfx.Console
	fb, graphics bool
	held         bool
}

func (s *screenOut) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held {
		return len(p), nil // the program owns the pixels; serial still has the text
	}
	return s.con.Write(p)
}

func (s *screenOut) hold(fb bool) { s.set(func() { s.fb = fb }) }

func (s *screenOut) holdGraphics(g bool) { s.set(func() { s.graphics = g }) }

// set changes a reason to stand aside and acts when the result changes.
func (s *screenOut) set(change func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change()
	held := s.fb || s.graphics
	if held == s.held {
		return
	}
	s.held = held
	if fb := bga.Screen(); held && fb != nil {
		fb.Fill(image.Rect(0, 0, fb.Width, fb.Height), gfx.RGB(0, 0, 0)) // a blank screen for the program
	}
	if !held {
		s.con = startScreen()
	}
}

// registerFB0 makes the framebuffer /dev/fb0.
func registerFB0(dev *devfs.FS, s *screenOut) error {
	m := bga.Current()
	f := m.Format
	mode := fbdev.Mode{Phys: m.Phys, Width: m.Width, Height: m.Height, PitchBytes: m.Pitch, BitsPerPixel: m.BPP,
		Red: fbdev.Channel{Offset: f.Red.Pos, Length: f.Red.Size}, Green: fbdev.Channel{Offset: f.Green.Pos, Length: f.Green.Size},
		Blue: fbdev.Channel{Offset: f.Blue.Pos, Length: f.Blue.Size}}
	return dev.Register("fb0", fbdev.New(mode, s.hold), 0o660)
}

// registerInput makes the input devices, /dev/input/event0 (the keyboard),
// event1 (the mouse) and mice (W14-S02), and returns the hooks the PS/2
// driver feeds (driver.Env.Scancodes and Mouse).
func registerInput(dev *devfs.FS, boot time.Time) (func(byte) bool, func(dx, dy, wheel int, buttons uint8)) {
	clock := func() time.Duration { return time.Since(boot) }
	kbd, mice := evdev.New(clock), evdev.NewMice()
	mouse := evdev.NewMouse(clock, mice)
	dev.Register("input/event0", kbd, 0o660)
	dev.Register("input/event1", mouse, 0o660)
	dev.Register("input/mice", mice, 0o660)
	return kbd.Feed, mouse.Feed
}
