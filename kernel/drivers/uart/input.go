package uart

import "burrow/kernel/driver"

// Input is the driver that feeds bytes received on a UART into the console
// (driver.Env.Input). The kernel registers it for the COM1 it already logs
// to, because that UART exists before the registry starts.
type Input struct {
	u    *UART
	name string
	irq  int
}

func NewInput(u *UART, name string, irq int) *Input { return &Input{u: u, name: name, irq: irq} }

func (in *Input) Name() string           { return in.name }
func (in *Input) Probe(*driver.Env) bool { return true } // Init already talked to it

func (in *Input) Start(env *driver.Env) error {
	in.u.EnableRX()
	driver.Serve(env.IRQ(in.irq), func() {
		for i := 0; i < 64; i++ { // the FIFO is 16 bytes; never spin forever
			b, ok := in.u.TryRead()
			if !ok {
				return
			}
			if env.Input != nil {
				env.Input <- b
			}
		}
	})
	env.Log.Infof("receive interrupts on irq %d", in.irq)
	return nil
}
