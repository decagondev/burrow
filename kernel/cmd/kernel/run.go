//go:build burrow

package main

import (
	"fmt"
	"io"

	"burrow/kernel/vfs"
)

// runProgram starts a user program in the foreground, like a shell: the
// console waits until it exits (typed keys queue up meanwhile).
func runProgram(k *kernel, args []string, out io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(out, "usage: run <program> [args...]")
		return
	}
	env := []string{"PATH=/bin", "HOME=/", "TERM=dumb"}
	p, err := k.procs.Spawn(vfs.Clean(args[0]), args, env)
	if err != nil {
		fmt.Fprintf(out, "run: %s: %v\n", args[0], err)
		return
	}
	status, reason := p.Wait()
	switch {
	case reason != "":
		fmt.Fprintf(out, "run: %s: killed (status %d)\n", args[0], status)
	case status != 0:
		fmt.Fprintf(out, "run: %s: exit status %d\n", args[0], status)
	}
}
