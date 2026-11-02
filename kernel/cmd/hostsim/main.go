// Command hostsim runs the Go kernel's console and drivers on simulated
// hardware as a normal program (kernel/hostsim): each line you type goes
// down the simulated serial line; `keys <text>` types on the simulated PS/2
// keyboard instead. `poweroff` or end of input quits.
//
//	make hostsim      (or: cd kernel && go run ./cmd/hostsim)
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"burrow/kernel/console"
	"burrow/kernel/hostsim"
)

func main() {
	m := hostsim.New()
	quit := make(chan struct{})
	m.Con.Add(console.Command{Name: "poweroff", Help: "leave hostsim", Run: func([]string, io.Writer) { close(quit) }})
	m.Start()
	go echoOutput(m)

	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	for {
		select {
		case <-quit:
			time.Sleep(20 * time.Millisecond) // let the last output through
			return
		case line, ok := <-lines:
			if !ok {
				return
			}
			prompts := m.Prompts()
			var err error
			if text, isKeys := strings.CutPrefix(line, "keys "); isKeys {
				err = m.TypeKeys(text + "\n")
			} else {
				err = m.TypeSerial(line + "\r")
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			waitPrompt(m, prompts) // one line at a time, even when stdin is a pipe
		}
	}
}

// waitPrompt returns once the console printed a prompt after the line just
// typed (it ran), or after a second.
func waitPrompt(m *hostsim.Machine, before int) {
	for deadline := time.Now().Add(time.Second); m.Prompts() <= before && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
}

// echoOutput copies what the simulated COM1 sends to stdout.
func echoOutput(m *hostsim.Machine) {
	shown := 0
	for {
		out := m.RawOutput()
		if len(out) > shown {
			os.Stdout.WriteString(out[shown:])
			shown = len(out)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
