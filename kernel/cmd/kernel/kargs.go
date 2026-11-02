//go:build burrow

package main

import (
	"fmt"
	"os"
)

// testFaults acts on words from the "kargs" boot module (argv after the
// program name). Regression tests use them to check that a Go panic and a
// nil dereference both fail the run loudly (W7-S06, PIT-07):
//
//	panic=boot  panic in a goroutine: the runtime prints it and every
//	            goroutine, then exit_group fails the run
//	fault=nil   write through a nil pointer: #PF in Go text, trap report
func testFaults() {
	for _, a := range os.Args[1:] {
		switch a {
		case "panic=boot":
			done := make(chan struct{})
			go func() {
				defer close(done)
				panic(fmt.Sprintf("kargs asked for a panic (%s)", a))
			}()
			<-done
		case "fault=nil":
			var p *int
			*p = 1
		}
	}
}
