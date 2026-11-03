// Package ps2 drives the 8042 PS/2 controller and the keyboard on its first
// port (E06.F03). Bring-up is polled; afterwards every IRQ 1 wakes the
// driver goroutine (driver.Serve), which drains the controller through
// Decoder into the console's input channel. Mouse (port 2, IRQ 12): later.
//
// Reference: the OSDev wiki "8042 PS/2 Controller" and "PS/2 Keyboard".
package ps2

import (
	"errors"
	"fmt"

	"burrow/kernel/driver"
	"burrow/kernel/hal"
)

const (
	IRQ = 1

	portData   uint16 = 0x60
	portStatus uint16 = 0x64 // read
	portCmd    uint16 = 0x64 // write

	stOutputFull = 1 << 0 // a byte waits in portData
	stInputFull  = 1 << 1 // the controller has not taken our last byte yet
	stAux        = 1 << 5 // the waiting byte is from port 2 (mouse)

	cmdReadConfig   = 0x20
	cmdWriteConfig  = 0x60
	cmdDisablePort2 = 0xA7
	cmdTestPort1    = 0xAB
	cmdSelfTest     = 0xAA
	cmdDisablePort1 = 0xAD
	cmdEnablePort1  = 0xAE

	cfgIRQ1        = 1 << 0
	cfgIRQ12       = 1 << 1
	cfgTranslation = 1 << 6 // the controller turns set 2 into set 1

	selfTestOK = 0x55
	portTestOK = 0x00
	kbdEnable  = 0xF4 // keyboard: start scanning
	kbdAck     = 0xFA
	pollLimit  = 100000
	flushLimit = 64
)

var errTimeout = errors.New("8042 did not answer")

// Controller is the 8042 behind hal.PortIO.
type Controller struct{ io hal.PortIO }

func NewController(io hal.PortIO) *Controller { return &Controller{io: io} }

func (c *Controller) waitWrite() error {
	for i := 0; i < pollLimit; i++ {
		if c.io.In8(portStatus)&stInputFull == 0 {
			return nil
		}
	}
	return errTimeout
}

func (c *Controller) read() (byte, error) {
	for i := 0; i < pollLimit; i++ {
		if c.io.In8(portStatus)&stOutputFull != 0 {
			return c.io.In8(portData), nil
		}
	}
	return 0, errTimeout
}

func (c *Controller) command(cmd byte) error {
	if err := c.waitWrite(); err != nil {
		return err
	}
	c.io.Out8(portCmd, cmd)
	return nil
}

func (c *Controller) write(port uint16, b byte) error {
	if err := c.waitWrite(); err != nil {
		return err
	}
	c.io.Out8(port, b)
	return nil
}

func (c *Controller) query(cmd byte) (byte, error) {
	if err := c.command(cmd); err != nil {
		return 0, err
	}
	return c.read()
}

// Init brings the controller and keyboard up: both ports off, buffer
// flushed, self-tests, then port 1 on with IRQ 1 and translation, and the
// keyboard told to scan.
func (c *Controller) Init() error {
	for _, cmd := range []byte{cmdDisablePort1, cmdDisablePort2} {
		if err := c.command(cmd); err != nil {
			return fmt.Errorf("disable ports: %w", err)
		}
	}
	for i := 0; i < flushLimit && c.io.In8(portStatus)&stOutputFull != 0; i++ {
		c.io.In8(portData)
	}
	cfg, err := c.query(cmdReadConfig)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	cfg &^= cfgIRQ1 | cfgIRQ12
	if err := c.setConfig(cfg); err != nil {
		return err
	}
	if r, err := c.query(cmdSelfTest); err != nil || r != selfTestOK {
		return fmt.Errorf("controller self-test: %#x, %v", r, err)
	}
	if r, err := c.query(cmdTestPort1); err != nil || r != portTestOK {
		return fmt.Errorf("port 1 test: %#x, %v", r, err)
	}
	if err := c.command(cmdEnablePort1); err != nil {
		return err
	}
	if err := c.setConfig(cfg | cfgIRQ1 | cfgTranslation); err != nil { // self-test may reset it
		return err
	}
	if err := c.write(portData, kbdEnable); err != nil {
		return err
	}
	if r, err := c.read(); err != nil || r != kbdAck {
		return fmt.Errorf("keyboard enable: %#x, %v", r, err)
	}
	return nil
}

func (c *Controller) setConfig(cfg byte) error {
	if err := c.command(cmdWriteConfig); err != nil {
		return err
	}
	return c.write(portData, cfg)
}

// Drain reads every waiting keyboard byte (mouse bytes are dropped).
func (c *Controller) Drain(each func(byte)) {
	for i := 0; i < flushLimit; i++ {
		st := c.io.In8(portStatus)
		if st&stOutputFull == 0 {
			return
		}
		b := c.io.In8(portData)
		if st&stAux == 0 {
			each(b)
		}
	}
}

func init() { driver.Register(NewDriver()) }

// NewDriver is the keyboard driver, for registries other than the kernel's
// (hostsim, tests).
func NewDriver() driver.Driver { return &keyboard{} }

type keyboard struct{}

func (*keyboard) Name() string { return "ps2" }

// Probe: with no controller the status port floats to 0xFF.
func (*keyboard) Probe(env *driver.Env) bool { return env.Ports.In8(portStatus) != 0xFF }

func (*keyboard) Start(env *driver.Env) error {
	c := NewController(env.Ports)
	if err := c.Init(); err != nil {
		return err
	}
	var dec Decoder
	driver.Serve(env.IRQ(IRQ), func() {
		c.Drain(func(sc byte) {
			grabbed := env.Scancodes != nil && env.Scancodes(sc)
			typed := dec.Feed(sc) // always, so modifiers stay right across a grab
			for _, b := range typed {
				if env.Input != nil && !grabbed {
					env.Input <- b
				}
			}
		})
	})
	env.Log.Infof("keyboard ready on irq %d (scancode set 1 via translation)", IRQ)
	return nil
}
