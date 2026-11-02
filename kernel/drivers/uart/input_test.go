package uart

import (
	"io"
	"testing"
	"time"

	"burrow/kernel/driver"
	"burrow/kernel/hal"
	"burrow/kernel/haltest"
	"burrow/kernel/klog"
)

func TestInputDeliversReceivedBytes(t *testing.T) {
	ports := haltest.NewFakePorts()
	rx := []byte("ok\r")
	ports.OnRead[COM1+regLSR] = func() uint8 {
		if len(rx) > 0 {
			return lsrDataReady | lsrTHREmpty
		}
		return lsrTHREmpty
	}
	ports.OnRead[COM1+regData] = func() uint8 { b := rx[0]; rx = rx[1:]; return b }
	q := haltest.NewFakeIRQ()
	defer q.Close()
	input := make(chan byte, 8)
	env := &driver.Env{
		Ports: ports, Input: input, Log: klog.New(io.Discard, klog.Info),
		IRQ: func(int) hal.IRQLine { return q },
	}
	if err := NewInput(New(ports, COM1), "com1", COM1IRQ).Start(env); err != nil {
		t.Fatal(err)
	}
	if ports.Values[COM1+regIER] != ierRXAvailable || ports.Values[COM1+regMCR]&mcrOut2 == 0 {
		t.Fatalf("IER=%#x MCR=%#x: receive interrupt not enabled", ports.Values[COM1+regIER], ports.Values[COM1+regMCR])
	}
	q.Fire()
	var got []byte
	for len(got) < 3 {
		select {
		case b := <-input:
			got = append(got, b)
		case <-time.After(2 * time.Second):
			t.Fatalf("got %q", got)
		}
	}
	if string(got) != "ok\r" {
		t.Fatalf("got %q, want \"ok\r\"", got)
	}
}
