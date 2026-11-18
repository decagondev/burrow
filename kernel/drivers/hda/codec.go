// codec.go reads a codec's widget graph and chooses an output path (spec
// §7.1-7.3): the audio function group's widgets, their capabilities,
// connection lists and pin configuration defaults, then a path from the
// best connected output pin (line out, then speaker, then headphones) back
// through mixers and selectors to a DAC, found by search, never by fixed
// node ids (PIT-108). Setting the path up powers it, selects each
// connection, unmutes each amplifier at its 0 dB step, enables the pin
// (and its EAPD) and tells the DAC its stream and format. Pure: it speaks
// to the codec only through Verber.
package hda

import (
	"errors"
	"fmt"
	"sort"
)

// Verber sends one verb (12-bit verb with 8-bit payload, or 4-bit verb
// with 16-bit payload, as the low 20 bits) to node nid of codec cad.
type Verber interface {
	Verb(cad, nid int, verb uint32) (uint32, error)
}

// Verbs and parameters (spec §7.3.3, §7.3.4).
const (
	verbGetParam    = 0xF00 << 8
	verbGetConnList = 0xF02 << 8
	verbGetConfig   = 0xF1C << 8
	verbSetSelect   = 0x701 << 8
	verbSetPower    = 0x705 << 8
	verbSetStream   = 0x706 << 8
	verbSetPinCtl   = 0x707 << 8
	verbSetEAPD     = 0x70C << 8
	verbSetFormat   = 0x2 << 16
	verbSetAmp      = 0x3 << 16
	paramVendor     = 0x00
	paramNodes      = 0x04
	paramFuncType   = 0x05
	paramWidgetCaps = 0x09
	paramPCM        = 0x0A
	paramPinCaps    = 0x0C
	paramInAmpCaps  = 0x0D
	paramConnLen    = 0x0E
	paramOutAmpCaps = 0x12
	funcAudio       = 0x01
	capInAmp        = 1 << 1
	capOutAmp       = 1 << 2
	capAmpOverride  = 1 << 3
	pinCapOut       = 1 << 4
	pinCapEAPD      = 1 << 16
	pinCtlOut       = 0x40
	pinCtlHP        = 0x80
	eapdOn          = 0x02
	ampOutLR        = 0xB000 // set output amp, left and right
	ampInLR         = 0x7000 // set input amp, left and right; index at 11:8
	connNone        = 1      // a pin default's port connectivity: nothing attached
	maxPathDepth    = 6
)

// Widget types (widget capabilities 23:20).
const (
	TypeOutput   = 0
	TypeInput    = 1
	TypeMixer    = 2
	TypeSelector = 3
	TypePin      = 4
)

// Widget is one node of an audio function group.
type Widget struct {
	NID     int
	Type    int
	Caps    uint32
	Conns   []int
	PinCaps uint32
	Config  uint32 // a pin's configuration default
}

// Device is a pin's default device (configuration default 23:20).
func (w Widget) Device() int { return int(w.Config>>20) & 0xF }

func (w Widget) connected() bool { return w.Config>>30 != connNone }

// Codec is one codec's audio function group.
type Codec struct {
	CAd     int
	Vendor  uint32
	AFG     int
	Widgets map[int]Widget
}

// ReadCodec walks codec cad's nodes to its audio function group.
func ReadCodec(v Verber, cad int) (*Codec, error) {
	vendor, err := v.Verb(cad, 0, verbGetParam|paramVendor)
	if err != nil {
		return nil, err
	}
	c := &Codec{CAd: cad, Vendor: vendor, Widgets: map[int]Widget{}}
	start, n, err := nodes(v, cad, 0)
	if err != nil {
		return nil, err
	}
	for fg := start; fg < start+n; fg++ {
		t, err := v.Verb(cad, fg, verbGetParam|paramFuncType)
		if err != nil || t&0xFF != funcAudio {
			continue
		}
		c.AFG = fg
		return c, c.readWidgets(v, fg)
	}
	return nil, fmt.Errorf("codec %d (%08x): no audio function group", cad, vendor)
}

func nodes(v Verber, cad, nid int) (start, n int, err error) {
	r, err := v.Verb(cad, nid, verbGetParam|paramNodes)
	return int(r>>16) & 0xFF, int(r) & 0xFF, err
}

func (c *Codec) readWidgets(v Verber, afg int) error {
	start, n, err := nodes(v, c.CAd, afg)
	if err != nil {
		return err
	}
	for nid := start; nid < start+n; nid++ {
		caps, err := v.Verb(c.CAd, nid, verbGetParam|paramWidgetCaps)
		if err != nil {
			return err
		}
		w := Widget{NID: nid, Type: int(caps>>20) & 0xF, Caps: caps}
		if w.Conns, err = connections(v, c.CAd, nid); err != nil {
			return err
		}
		if w.Type == TypePin {
			w.PinCaps, _ = v.Verb(c.CAd, nid, verbGetParam|paramPinCaps)
			w.Config, _ = v.Verb(c.CAd, nid, verbGetConfig)
		}
		c.Widgets[nid] = w
	}
	return nil
}

// connections reads a node's connection list, short (8-bit entries, four
// a response) or long form (16-bit, two), ranges expanded.
func connections(v Verber, cad, nid int) ([]int, error) {
	l, err := v.Verb(cad, nid, verbGetParam|paramConnLen)
	if err != nil {
		return nil, err
	}
	n, long := int(l&0x7F), l&0x80 != 0
	per, bits := 4, 8
	if long {
		per, bits = 2, 16
	}
	var out []int
	for i := 0; i < n; i += per {
		r, err := v.Verb(cad, nid, verbGetConnList|uint32(i))
		if err != nil {
			return nil, err
		}
		for j := 0; j < per && i+j < n; j++ {
			e := int(r>>(bits*j)) & (1<<bits - 1)
			id, isRange := e&(1<<(bits-1)-1), e&(1<<(bits-1)) != 0
			if isRange && len(out) > 0 {
				for k := out[len(out)-1] + 1; k < id; k++ {
					out = append(out, k)
				}
			}
			out = append(out, id)
		}
	}
	return out, nil
}

// Path is an output path: Nodes[0] is the pin, the last the DAC; Index[i]
// is which connection of Nodes[i] leads to Nodes[i+1].
type Path struct {
	Nodes []int
	Index []int
}

var ErrNoPath = errors.New("hda: no output pin reaches a DAC")

// pinRank orders the default devices: line out, speaker, headphones.
var pinRank = map[int]int{0: 0, 1: 1, 2: 2}

// OutputPath is the path from the best connected output pin to a DAC.
func (c *Codec) OutputPath() (Path, error) {
	var pins []Widget
	for _, w := range c.Widgets {
		if _, ok := pinRank[w.Device()]; ok && w.Type == TypePin && w.PinCaps&pinCapOut != 0 && w.connected() {
			pins = append(pins, w)
		}
	}
	sort.Slice(pins, func(i, j int) bool {
		if a, b := pinRank[pins[i].Device()], pinRank[pins[j].Device()]; a != b {
			return a < b
		}
		return pins[i].NID < pins[j].NID
	})
	for _, p := range pins {
		if path, ok := c.search(p.NID, map[int]bool{}, 0); ok {
			return path, nil
		}
	}
	return Path{}, ErrNoPath
}

func (c *Codec) search(nid int, seen map[int]bool, depth int) (Path, bool) {
	w, ok := c.Widgets[nid]
	if !ok || seen[nid] || depth > maxPathDepth {
		return Path{}, false
	}
	if w.Type == TypeOutput {
		return Path{Nodes: []int{nid}}, true
	}
	if depth > 0 && w.Type != TypeMixer && w.Type != TypeSelector {
		return Path{}, false
	}
	seen[nid] = true
	defer delete(seen, nid)
	for i, next := range w.Conns {
		if rest, ok := c.search(next, seen, depth+1); ok {
			return Path{Nodes: append([]int{nid}, rest.Nodes...), Index: append([]int{i}, rest.Index...)}, true
		}
	}
	return Path{}, false
}

// String names the path as "pin 3 (line out) <- 2 (DAC)".
func (c *Codec) String(p Path) string {
	names := map[int]string{0: "line out", 1: "speaker", 2: "headphones"}
	s := ""
	for i, nid := range p.Nodes {
		w := c.Widgets[nid]
		switch {
		case i == 0:
			s = fmt.Sprintf("pin %d (%s)", nid, names[w.Device()])
		case w.Type == TypeOutput:
			s += fmt.Sprintf(" <- dac %d", nid)
		case w.Type == TypeMixer:
			s += fmt.Sprintf(" <- mixer %d", nid)
		default:
			s += fmt.Sprintf(" <- selector %d", nid)
		}
	}
	return s
}
