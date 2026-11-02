package console

import (
	"fmt"
	"io"
	"io/fs"
	"strings"

	"burrow/kernel/vfs"
)

// FileCommands are the console's file tools over a VFS (W8-S03). Paths are
// absolute ("/etc/motd"); there is no working directory until processes.
func FileCommands(v *vfs.VFS) []Command {
	return []Command{
		{"ls", "list a directory (default /)", func(args []string, out io.Writer) {
			if len(args) == 0 {
				args = []string{"/"}
			}
			for _, a := range args {
				ls(v, a, out)
			}
		}},
		{"cat", "print files", func(args []string, out io.Writer) {
			for _, a := range args {
				b, err := fs.ReadFile(v, vfs.Clean(a))
				if err != nil {
					fmt.Fprintf(out, "cat: %v\n", err)
					continue
				}
				out.Write(b)
			}
		}},
		{"write", "write <file> <text...>: replace a file's contents", func(args []string, out io.Writer) {
			if len(args) < 1 {
				fmt.Fprintln(out, "usage: write <file> <text...>")
				return
			}
			text := strings.Join(args[1:], " ") + "\n"
			if err := v.WriteFile(vfs.Clean(args[0]), []byte(text)); err != nil {
				fmt.Fprintf(out, "write: %v\n", err)
			}
		}},
		{"mkdir", "make directories", func(args []string, out io.Writer) {
			for _, a := range args {
				if err := v.Mkdir(vfs.Clean(a)); err != nil {
					fmt.Fprintf(out, "mkdir: %v\n", err)
				}
			}
		}},
		{"rm", "remove files or empty directories", func(args []string, out io.Writer) {
			for _, a := range args {
				if err := v.Remove(vfs.Clean(a)); err != nil {
					fmt.Fprintf(out, "rm: %v\n", err)
				}
			}
		}},
		{"mounts", "list mounted file systems", func(_ []string, out io.Writer) {
			for _, m := range v.Mounts() {
				fmt.Fprintln(out, m)
			}
		}},
	}
}

func ls(v *vfs.VFS, p string, out io.Writer) {
	name := vfs.Clean(p)
	st, err := fs.Stat(v, name)
	if err != nil {
		fmt.Fprintf(out, "ls: %v\n", err)
		return
	}
	if !st.IsDir() {
		fmt.Fprintf(out, "%s %8d %s\n", st.Mode(), st.Size(), st.Name())
		return
	}
	ents, err := fs.ReadDir(v, name)
	if err != nil {
		fmt.Fprintf(out, "ls: %v\n", err)
		return
	}
	for _, e := range ents {
		info, err := e.Info()
		if err != nil {
			continue
		}
		suffix := ""
		if e.IsDir() {
			suffix = "/"
		}
		fmt.Fprintf(out, "%s %8d %s%s\n", info.Mode(), info.Size(), e.Name(), suffix)
	}
}
