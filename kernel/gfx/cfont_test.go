package gfx

// cfont_test.go - the C copy of the console font (common/font5x8.c, W26-S04)
// that the loader and the nucleus draw with must match glyphSource: a
// changed glyph here fails until the C file is changed too.

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

var cGlyphLine = regexp.MustCompile(`^\s*\{((?:0x[0-9A-F]{2}, ){7}0x[0-9A-F]{2})\},\s+/\* 0x([0-9a-f]{2}) `)

func TestCFontMatchesGo(t *testing.T) {
	src, err := os.ReadFile("../../common/font5x8.c")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, line := range regexp.MustCompile(`\n`).Split(string(src), -1) {
		m := cGlyphLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		c, _ := strconv.ParseUint(m[2], 16, 8)
		bytes := regexp.MustCompile(`0x([0-9A-F]{2})`).FindAllStringSubmatch(m[1], -1)
		for r, b := range bytes {
			v, _ := strconv.ParseUint(b[1], 16, 8)
			if uint8(v) != glyphs[c][r] {
				t.Errorf("glyph %q row %d: C has %#02x, Go %#02x", rune(c), r, v, glyphs[c][r])
			}
		}
		seen++
	}
	if seen != 0x7E-0x20+1 {
		t.Fatalf("common/font5x8.c: %d glyphs, want %d", seen, 0x7E-0x20+1)
	}
}
