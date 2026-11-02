package console

import (
	"io"
	"strings"
	"testing"
)

func edit(input string) (echo string, lines []string) {
	var e Editor
	var out []byte
	for i := 0; i < len(input); i++ {
		ech, line, done := e.Feed(input[i])
		out = append(out, ech...)
		if done {
			lines = append(lines, line)
		}
	}
	return string(out), lines
}

func TestEditor(t *testing.T) {
	for _, c := range []struct {
		name, in, echo string
		lines          []string
	}{
		{"plain line", "ls -l\r", "ls -l\n", []string{"ls -l"}},
		{"CR LF ends once", "a\r\nb\n", "a\nb\n", []string{"a", "b"}},
		{"backspace and DEL", "abx\x7fc\bd\r", "abx\b \bc\b \bd\n", []string{"abd"}},
		{"backspace on empty line", "\x7f\r", "\n", []string{""}},
		{"ctrl-U kills the line", "abc\x15ok\r", "abc\b \b\b \b\b \bok\n", []string{"ok"}},
		{"ctrl-C abandons", "rm\x03x\r", "rm^C\nx\n", []string{"", "x"}},
		{"arrow keys are swallowed", "a\x1b[Ab\x1b[3~c\r", "abc\n", []string{"abc"}},
		{"other control bytes ignored", "a\x01\tb\r", "ab\n", []string{"ab"}},
	} {
		echo, lines := edit(c.in)
		if echo != c.echo || strings.Join(lines, "|") != strings.Join(c.lines, "|") {
			t.Errorf("%s: %q echoed %q lines %q, want %q %q", c.name, c.in, echo, lines, c.echo, c.lines)
		}
	}
}

func TestEditorLineLimit(t *testing.T) {
	_, lines := edit(strings.Repeat("x", MaxLine+10) + "\r")
	if len(lines) != 1 || len(lines[0]) != MaxLine {
		t.Fatalf("line of %d bytes kept, want %d", len(lines[0]), MaxLine)
	}
}

func TestConsoleRunsCommands(t *testing.T) {
	var out strings.Builder
	in := make(chan byte, 128)
	c := New(&out, in)
	c.Add(Command{"greet", "say hi", func(args []string, w io.Writer) {
		io.WriteString(w, "hi "+strings.Join(args, ",")+"\n")
	}})
	for _, b := range []byte("echo  a   b\rgreet x y\rnope\rhelp\r") {
		in <- b
	}
	close(in)
	c.Run()
	for _, want := range []string{
		Prompt + "echo  a   b\na b\n",
		Prompt + "greet x y\nhi x,y\n",
		"nope: unknown command (try help)\n",
		"  echo      print the arguments\n  greet     say hi\n  help      list commands\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}
