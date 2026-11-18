package xhci

import (
	"testing"

	"burrow/kernel/usb"
)

// TestRouteStrings: each tier's port is one nibble, the first tier lowest;
// a port above 15 is written as 15 (spec §8.9).
func TestRouteStrings(t *testing.T) {
	r1 := childRoute(0, 0, 3)
	r2 := childRoute(r1, 1, 2)
	r3 := childRoute(r2, 2, 20)
	if r1 != 0x3 || r2 != 0x23 || r3 != 0xF23 {
		t.Fatalf("routes %#x %#x %#x", r1, r2, r3)
	}
}

// TestChildPlace: a low- or full-speed device behind a high-speed hub uses
// that hub's TT; behind a full-speed hub below it, still that one; a
// high-speed device none (PIT-107).
func TestChildPlace(t *testing.T) {
	hs := &Device{Slot: 4, Speed: usb.SpeedHigh, place: place{rootPort: 6, path: "6"}}
	kbd := hs.childPlace(2, usb.SpeedLow)
	if kbd.rootPort != 6 || kbd.route != 0x2 || kbd.depth != 1 || kbd.path != "6.2" || kbd.ttSlot != 4 || kbd.ttPort != 2 {
		t.Fatalf("low-speed behind a high-speed hub: %+v", kbd)
	}
	if fast := hs.childPlace(3, usb.SpeedHigh); fast.ttSlot != 0 || fast.ttPort != 0 {
		t.Fatalf("high-speed: %+v", fast)
	}
	fs := &Device{Slot: 7, Speed: usb.SpeedFull, place: kbd} // a full-speed hub on that port
	mouse := fs.childPlace(1, usb.SpeedFull)
	if mouse.route != 0x12 || mouse.depth != 2 || mouse.path != "6.2.1" || mouse.ttSlot != 4 || mouse.ttPort != 2 {
		t.Fatalf("behind a full-speed hub behind a high-speed one: %+v", mouse)
	}
	root := &Device{Slot: 1, Speed: usb.SpeedFull, place: place{rootPort: 5, path: "5"}}
	if p := root.childPlace(1, usb.SpeedFull); p.ttSlot != 0 || p.route != 1 {
		t.Fatalf("behind a full-speed hub on a root port: %+v", p)
	}
}

// TestHubSlotContext: a hub's slot context carries Hub, its ports and its
// TT think time; a device behind it its route and TT.
func TestHubSlotContext(t *testing.T) {
	for _, l := range []layout{32, 64} {
		page := make([]byte, pageBytes)
		l.slotContext(page, slotInfo{rootPort: 6, route: 0x21, speed: usb.SpeedHigh, lastDCI: 3,
			hub: true, ports: 4, thinkTime: 2})
		s := page[int(l):]
		if le.Uint32(s) != 0x21|3<<20|1<<26|3<<27 || le.Uint32(s[4:]) != 6<<16|4<<24 || le.Uint32(s[8:]) != 2<<16 {
			t.Fatalf("%d: hub %#x %#x %#x", l, le.Uint32(s), le.Uint32(s[4:]), le.Uint32(s[8:]))
		}
		l.slotContext(page, slotInfo{rootPort: 6, route: 0x1, speed: usb.SpeedLow, lastDCI: 1, ttSlot: 4, ttPort: 1})
		if le.Uint32(s) != 0x1|2<<20|1<<27 || le.Uint32(s[4:]) != 6<<16 || le.Uint32(s[8:]) != 4|1<<8 {
			t.Fatalf("%d: device %#x %#x %#x", l, le.Uint32(s), le.Uint32(s[4:]), le.Uint32(s[8:]))
		}
	}
}
