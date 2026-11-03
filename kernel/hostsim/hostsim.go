// Package hostsim runs the Go kernel's driver stack as an ordinary host
// process (E06.F06): the real driver registry, PS/2 and serial drivers and
// console, on simulated hardware. A fake 8042, a fake 16550 and
// channel-driven IRQ lines stand in for the PC, so typing at the simulated
// keyboard exercises the same code paths as QEMU, in milliseconds and
// under the race detector.
//
// Not simulated: the nucleus, the PIT, PCI. Those stay QEMU's job.
package hostsim

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"time"

	"burrow/kernel/console"
	"burrow/kernel/driver"
	"burrow/kernel/drivers/ps2"
	"burrow/kernel/drivers/uart"
	"burrow/kernel/fs/memfs"
	"burrow/kernel/hal"
	"burrow/kernel/haltest"
	"burrow/kernel/klog"
	"burrow/kernel/vfs"
)

// Machine is one simulated PC with the console running on it.
type Machine struct {
	kbc    *haltest.Fake8042
	com1   *fakeUART
	irqs   map[int]*haltest.FakeIRQ
	input  chan byte
	Con    *console.Console // add commands before Start
	FS     *vfs.VFS         // / (a small read-only tree) and /tmp
	Log    *klog.Logger
	closed chan struct{}
}

// New builds the machine; Start powers it on.
func New() *Machine {
	m := &Machine{
		kbc:    haltest.NewFake8042(),
		com1:   newFakeUART(uart.COM1),
		irqs:   map[int]*haltest.FakeIRQ{},
		input:  make(chan byte, 256),
		closed: make(chan struct{}),
	}
	for _, line := range []int{ps2.IRQ, ps2.MouseIRQ, uart.COM1IRQ} {
		m.irqs[line] = haltest.NewFakeIRQ()
	}
	com1 := uart.New(m, uart.COM1)
	com1.Init()
	m.Log = klog.New(com1, klog.Info)
	m.Con = console.New(m.Log.Writer(), m.input)
	m.FS = vfs.New()
	root := memfs.New()
	root.MkdirAll("etc", 0o755, time.Time{})
	root.WriteFile("etc/motd", []byte("Welcome to Burrow (hostsim).\n"), 0o644, time.Time{})
	root.Freeze()
	m.FS.Mount(".", root, "memfs")
	m.FS.Mount("tmp", memfs.New(), "ramfs")
	for _, cmd := range console.FileCommands(m.FS) {
		m.Con.Add(cmd)
	}
	return m
}

// Start runs the drivers and the console (in a goroutine).
func (m *Machine) Start() []string {
	r := driver.NewRegistry()
	r.Register(ps2.NewDriver())
	r.Register(uart.NewInput(uart.New(m, uart.COM1), "serial", uart.COM1IRQ))
	started := r.StartAll(&driver.Env{
		Ports: m,
		IRQ:   func(line int) hal.IRQLine { return m.irqs[line] },
		Log:   m.Log,
		Input: m.input,
	})
	go m.Con.Run()
	return started
}

// Stop closes the IRQ lines, which ends the drivers' Serve goroutines.
func (m *Machine) Stop() {
	for _, q := range m.irqs {
		q.Close()
	}
}

// In8/Out8 route port I/O to the simulated devices (Machine is the PortIO).
func (m *Machine) In8(port uint16) uint8 {
	if m.com1.owns(port) {
		return m.com1.In8(port)
	}
	return m.kbc.In8(port)
}

func (m *Machine) Out8(port uint16, v uint8) {
	if m.com1.owns(port) {
		m.com1.Out8(port, v)
		return
	}
	m.kbc.Out8(port, v)
}

// TypeKeys presses each character on the PS/2 keyboard, one interrupt per
// key, and waits until the driver has read it before the next (a real 8042
// buffers one byte, so a keyboard cannot run ahead of its driver either).
func (m *Machine) TypeKeys(text string) error {
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == '\n' {
			c = '\r'
		}
		codes := ps2.Encode(c)
		if codes == nil {
			return fmt.Errorf("hostsim: no key types %q", c)
		}
		m.kbc.Send(false, codes...)
		m.irqs[ps2.IRQ].Fire()
		deadline := time.Now().Add(time.Second)
		for {
			if _, _, _, waiting := m.kbc.State(); waiting == 0 {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("hostsim: the keyboard driver did not read %q", c)
			}
			time.Sleep(100 * time.Microsecond)
		}
	}
	return nil
}

// TypeSerial sends text down the serial line, as a terminal would.
// Like TypeKeys it returns once the driver has read every byte.
func (m *Machine) TypeSerial(text string) error {
	m.com1.receive([]byte(text))
	m.irqs[uart.COM1IRQ].Fire()
	deadline := time.Now().Add(time.Second)
	for m.com1.waiting() > 0 {
		if time.Now().After(deadline) {
			return fmt.Errorf("hostsim: the serial driver did not read %q", text)
		}
		time.Sleep(100 * time.Microsecond)
	}
	return nil
}

// Prompts counts console prompts so far: one more means the last line ran.
func (m *Machine) Prompts() int { return strings.Count(m.RawOutput(), console.Prompt) }

// Output is everything written to COM1 so far, with "\r\n" as "\n".
func (m *Machine) Output() string {
	return strings.ReplaceAll(m.com1.sent(), "\r\n", "\n")
}

// RawOutput is exactly what COM1 sent, line endings and all.
func (m *Machine) RawOutput() string { return m.com1.sent() }

// WaitFor returns once want appears in Output, or fails after timeout.
func (m *Machine) WaitFor(want string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(m.Output(), want) {
			return nil
		}
		time.Sleep(time.Millisecond)
	}
	return fmt.Errorf("hostsim: %q not in output after %v:\n%s", want, timeout, m.Output())
}

// fakeUART is a 16550 at base: transmit always ready, received bytes from
// receive(), interrupt enables remembered.
type fakeUART struct {
	mu   sync.Mutex
	base uint16
	tx   bytes.Buffer
	rx   []byte
	regs [8]uint8
}

func newFakeUART(base uint16) *fakeUART { return &fakeUART{base: base} }

const (
	uartLSR       = 5
	lsrDataReady  = 0x01
	lsrTHREmpty   = 0x20
	uartLCR       = 3
	lcrDivisorBit = 0x80
)

func (u *fakeUART) owns(port uint16) bool { return port >= u.base && port < u.base+8 }

func (u *fakeUART) In8(port uint16) uint8 {
	u.mu.Lock()
	defer u.mu.Unlock()
	switch port - u.base {
	case 0:
		if len(u.rx) == 0 {
			return 0
		}
		b := u.rx[0]
		u.rx = u.rx[1:]
		return b
	case uartLSR:
		st := uint8(lsrTHREmpty)
		if len(u.rx) > 0 {
			st |= lsrDataReady
		}
		return st
	}
	return u.regs[port-u.base]
}

func (u *fakeUART) Out8(port uint16, v uint8) {
	u.mu.Lock()
	defer u.mu.Unlock()
	reg := port - u.base
	if reg == 0 && u.regs[uartLCR]&lcrDivisorBit == 0 {
		u.tx.WriteByte(v)
		return
	}
	u.regs[reg] = v
}

func (u *fakeUART) waiting() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.rx)
}

func (u *fakeUART) receive(b []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.rx = append(u.rx, b...)
}

func (u *fakeUART) sent() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.tx.String()
}
