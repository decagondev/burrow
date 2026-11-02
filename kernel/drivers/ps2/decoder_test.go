package ps2

import (
	"fmt"
	"testing"
)

func feed(d *Decoder, codes ...byte) string {
	var out []byte
	for _, c := range codes {
		out = append(out, d.Feed(c)...)
	}
	return string(out)
}

func TestDecoder(t *testing.T) {
	for _, c := range []struct {
		name  string
		codes []byte
		want  string
	}{
		{"letters and release", []byte{0x23, 0xA3, 0x17, 0x97}, "hi"},
		{"digits space enter", []byte{0x02, 0x82, 0x39, 0xB9, 0x0B, 0x8B, 0x1C, 0x9C}, "1 0\r"},
		{"left shift", []byte{0x2A, 0x23, 0xA3, 0xAA, 0x17}, "Hi"},
		{"right shift symbols", []byte{0x36, 0x02, 0x0D, 0x28, 0xB6, 0x28}, "!+\"'"},
		{"caps lock letters only", []byte{0x3A, 0xBA, 0x1E, 0x02, 0x2A, 0x1E, 0xAA, 0x3A, 0x1E}, "A1aa"},
		{"ctrl letters", []byte{0x1D, 0x2E, 0x16, 0x9D, 0x2E}, "\x03\x15c"},
		{"right ctrl via e0", []byte{0xE0, 0x1D, 0x26, 0xE0, 0x9D, 0x26}, "\x0cl"},
		{"arrows and delete", []byte{0xE0, 0x48, 0xE0, 0xC8, 0xE0, 0x4B, 0xE0, 0x53}, "\x1b[A\x1b[D\x1b[3~"},
		{"fake shift around print screen", []byte{0xE0, 0x2A, 0x1E, 0xE0, 0xAA}, "a"},
		{"backspace tab escape", []byte{0x0E, 0x0F, 0x01}, "\x7f\t\x1b"},
		{"unknown codes are ignored", []byte{0x3B, 0x58, 0x7F}, ""},
	} {
		var d Decoder
		if got := feed(&d, c.codes...); got != c.want {
			t.Errorf("%s: % x typed %q, want %q", c.name, c.codes, got, c.want)
		}
	}
}

// Every letter key maps to its letter in both tables.
func TestDecoderTablesAgree(t *testing.T) {
	for code, s := range plain {
		if len(s) == 1 && s[0] >= 'a' && s[0] <= 'z' {
			if shifted[code] != string(s[0]-'a'+'A') {
				t.Errorf("code %#x: plain %q, shifted %q", code, s, shifted[code])
			}
		}
		if (plain[code] == "") != (shifted[code] == "") {
			t.Errorf("code %#x: only one table has it", code)
		}
	}
	if got := fmt.Sprint(len(plain)); got != "58" {
		t.Errorf("table covers %s codes, want 0x00-0x39", got)
	}
}
