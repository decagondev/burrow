package devfs

import (
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"
	"time"
)

func devices(t *testing.T) (*FS, *strings.Builder) {
	t.Helper()
	var console strings.Builder
	d := New()
	for name, dev := range map[string]Device{
		"null": Null{}, "zero": Zero{}, "console": WriteOnly{&console}, "input/event0": Null{},
	} {
		if err := d.Register(name, dev, 0o666); err != nil {
			t.Fatal(err)
		}
	}
	return d, &console
}

func TestListAndStat(t *testing.T) {
	d, _ := devices(t)
	ents, err := fs.ReadDir(d, ".")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range ents {
		got = append(got, e.Name()+":"+e.Type().String())
	}
	if want := "console:Dc---------,input:d---------,null:Dc---------,zero:Dc---------"; strings.Join(got, ",") != want {
		t.Errorf("ReadDir(.) = %v, want %s", got, want)
	}
	if ents, _ := fs.ReadDir(d, "input"); len(ents) != 1 || ents[0].Name() != "event0" {
		t.Errorf("ReadDir(input) = %v", ents)
	}
	if st, err := fs.Stat(d, "null"); err != nil || st.Mode()&fs.ModeCharDevice == 0 || st.Name() != "null" {
		t.Errorf("Stat(null) = %v, %v", st, err)
	}
	if _, err := fs.Stat(d, "nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat(nope) = %v", err)
	}
}

func TestDevicesBehave(t *testing.T) {
	d, console := devices(t)
	if b, err := fs.ReadFile(d, "null"); err != nil || len(b) != 0 {
		t.Errorf("null reads %q, %v", b, err)
	}
	f, _ := d.Open("zero")
	buf := []byte("xxxx")
	if n, err := f.Read(buf); n != 4 || err != nil || string(buf) != "\x00\x00\x00\x00" {
		t.Errorf("zero read %d %q %v", n, buf, err)
	}
	if err := d.WriteFile("console", []byte("hi\n"), 0, time.Time{}); err != nil || console.String() != "hi\n" {
		t.Errorf("console got %q, %v", console.String(), err)
	}
	if w, ok := f.(io.Writer); !ok {
		t.Error("an open device file is not an io.Writer")
	} else if n, _ := w.Write([]byte("abc")); n != 3 {
		t.Errorf("zero swallowed %d of 3 bytes", n)
	}
	for _, err := range []error{
		d.WriteFile("newfile", nil, 0, time.Time{}),
		d.Mkdir("dir", 0, time.Time{}),
		d.Remove("null"),
	} {
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("got %v, want permission denied", err)
		}
	}
	if err := d.Register("null", Null{}, 0o666); !errors.Is(err, fs.ErrExist) {
		t.Errorf("duplicate Register = %v", err)
	}
	if err := d.Register("input", Null{}, 0o666); !errors.Is(err, fs.ErrExist) {
		t.Errorf("Register over a directory = %v", err)
	}
}

// counted counts its open handles (an Opener).
type counted struct {
	Null
	opens *int
}

func (c counted) Open() (Device, error) { *c.opens++; return c, nil }
func (c counted) Close() error          { *c.opens--; return nil }

func TestOpenerGetsEachOpen(t *testing.T) {
	d := New()
	opens := 0
	d.Register("fb0", counted{opens: &opens}, 0o660)
	a, _ := d.Open("fb0")
	b, _ := d.Open("fb0")
	if opens != 2 {
		t.Fatalf("%d opens, want 2", opens)
	}
	if _, ok := a.(interface{ Device() Device }).Device().(counted); !ok {
		t.Error("Device() is not the handle Open returned")
	}
	a.Close()
	b.Close()
	if opens != 0 {
		t.Errorf("%d opens after closing both", opens)
	}
}
