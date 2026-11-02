package console

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Prompt starts every input line.
const Prompt = "burrow> "

// Command is one console command. Run writes its output to out.
type Command struct {
	Name string
	Help string
	Run  func(args []string, out io.Writer)
}

// Console reads In, echoes to Out and runs commands.
type Console struct {
	out      io.Writer
	in       <-chan byte
	commands map[string]Command
	ed       Editor
}

// New makes a console with the built-in commands help and echo; the kernel
// adds the ones that need hardware or the runtime.
func New(out io.Writer, in <-chan byte) *Console {
	c := &Console{out: out, in: in, commands: map[string]Command{}}
	c.Add(Command{"help", "list commands", c.help})
	c.Add(Command{"echo", "print the arguments", func(args []string, out io.Writer) {
		fmt.Fprintln(out, strings.Join(args, " "))
	}})
	return c
}

// Add registers (or replaces) a command.
func (c *Console) Add(cmd Command) { c.commands[cmd.Name] = cmd }

// Run serves the console until In is closed.
func (c *Console) Run() {
	io.WriteString(c.out, Prompt)
	for b := range c.in {
		echo, line, done := c.ed.Feed(b)
		c.out.Write(echo)
		if done {
			c.Exec(line)
			io.WriteString(c.out, Prompt)
		}
	}
}

// Exec runs one line: the first word names the command.
func (c *Console) Exec(line string) {
	words := strings.Fields(line)
	if len(words) == 0 {
		return
	}
	cmd, ok := c.commands[words[0]]
	if !ok {
		fmt.Fprintf(c.out, "%s: unknown command (try help)\n", words[0])
		return
	}
	cmd.Run(words[1:], c.out)
}

func (c *Console) help(_ []string, out io.Writer) {
	names := make([]string, 0, len(c.commands))
	for n := range c.commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(out, "  %-9s %s\n", n, c.commands[n].Help)
	}
}
