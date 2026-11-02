package uart

import (
	"bytes"
	"fmt"
	"testing"

	"burrow/kernel/haltest"
)

func readyPorts() *haltest.FakePorts {
	p := haltest.NewFakePorts()
	p.OnRead[COM1+regLSR] = func() uint8 { return lsrTHREmpty } // always ready
	return p
}

func TestInitProgramsBaudAndFormat(t *testing.T) {
	p := readyPorts()
	New(p, COM1).Init()
	w := func(port uint16, v uint8) haltest.Write { return haltest.Write{Port: port, Value: v} }
	want := []haltest.Write{
		w(COM1+regIER, 0x00), w(COM1+regLCR, 0x80), w(COM1+regData, 0x01),
		w(COM1+regIER, 0x00), w(COM1+regLCR, 0x03), w(COM1+regFCR, 0xC7),
		w(COM1+regMCR, 0x03),
	}
	if fmt.Sprint(p.Writes) != fmt.Sprint(want) {
		t.Fatalf("init sequence\n got %v\nwant %v", p.Writes, want)
	}
}

func TestWriteTranslatesNewline(t *testing.T) {
	p := readyPorts()
	u := New(p, COM1)
	fmt.Fprintf(u, "hi\n")
	if got := p.BytesWrittenTo(COM1 + regData); !bytes.Equal(got, []byte("hi\r\n")) {
		t.Fatalf("got %q", got)
	}
}

func TestWaitsForTransmitterEmpty(t *testing.T) {
	p := haltest.NewFakePorts()
	busy := 3
	p.OnRead[COM1+regLSR] = func() uint8 {
		if busy > 0 {
			busy--
			return 0
		}
		return lsrTHREmpty
	}
	_ = New(p, COM1).WriteByte('x')
	if busy != 0 {
		t.Fatal("driver wrote before the transmitter reported empty")
	}
}
