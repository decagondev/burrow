//go:build burrow

package main

import (
	"image"
	"io"
	"sync"

	"burrow/kernel/drivers/bga"
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
