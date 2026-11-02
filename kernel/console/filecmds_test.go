package console

import (
	"strings"
	"testing"
	"time"

	"burrow/kernel/fs/memfs"
	"burrow/kernel/vfs"
)

func TestFileCommands(t *testing.T) {
	root := memfs.New()
	root.MkdirAll("etc", 0o755, time.Time{})
	root.WriteFile("etc/motd", []byte("Welcome\n"), 0o644, time.Time{})
	root.Freeze()
	v := vfs.New()
	v.Mount(".", root, "tarfs")
	v.Mount("tmp", memfs.New(), "ramfs")

	var out strings.Builder
	c := New(&out, nil)
	for _, cmd := range FileCommands(v) {
		c.Add(cmd)
	}
	for _, line := range []string{
		"cat /etc/motd", "write /tmp/note hi  there", "cat /tmp/note", "ls /",
		"write /etc/x no", "mkdir /tmp/d", "rm /tmp", "cat /nope", "mounts",
	} {
		c.Exec(line)
	}
	for _, want := range []string{
		"Welcome\n",
		"hi there\n",
		"drwxr-xr-x        0 etc/\n",
		"drwxr-xr-x        0 tmp/\n",
		"write: write etc/x: permission denied\n",
		"rm: remove tmp: permission denied\n",
		"cat: open nope: file does not exist\n",
		"tarfs    on /\nramfs    on /tmp\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}
