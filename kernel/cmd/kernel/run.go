//go:build burrow

package main

import (
	"fmt"
	"io"
	"time"

	"burrow/kernel/vfs"
)

// runProgram starts a user program in the foreground, like a shell: the
// terminal belongs to its process group until it exits.
func runProgram(k *kernel, args []string, out io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(out, "usage: run <program> [args...]")
		return
	}
	env := append([]string{"PATH=/bin", "HOME=/", "TERM=dumb"}, k.env...)
	p, err := k.procs.Spawn(vfs.Clean(args[0]), args, env)
	if err != nil {
		fmt.Fprintf(out, "run: %s: %v\n", args[0], err)
		return
	}
	k.tty.SetForeground(p.PGID)
	stop := make(chan []byte)
	go forwardInput(k, stop)
	status, reason := p.Wait()
	stop <- nil
	k.con.Unread(<-stop) // typed ahead, never read: back to the console
	k.tty.SetForeground(0)
	switch {
	case reason != "":
		fmt.Fprintf(out, "run: %s: killed (status %d)\n", args[0], status)
	case p.TermSig() != 0: // exit(255) is status 255 too, so ask
		fmt.Fprintf(out, "run: %s: killed by signal %d\n", args[0], p.TermSig())
	case status != 0:
		fmt.Fprintf(out, "run: %s: exit status %d\n", args[0], status)
	}
}

// forwardInput gives the terminal what the running program may see: ^C
// and friends at once, other keys only while it waits in read (one line at
// a time). On stop it answers with the keys it held back.
func forwardInput(k *kernel, stop chan []byte) {
	var held []byte
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case b := <-k.input:
			if k.tty.IsSignal(b) {
				k.tty.Input(b)
			} else {
				held = append(held, b)
			}
		case <-tick.C:
		case <-stop:
			stop <- held
			return
		}
		for len(held) > 0 && k.tty.HasReader() {
			b := held[0]
			held = held[1:]
			k.tty.Input(b)
			if b == '\r' || b == '\n' || b == 4 {
				break // the reader returns with this line: wait for its next read
			}
		}
	}
}
