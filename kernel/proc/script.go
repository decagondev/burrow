package proc

// script.go - "#!" scripts for execve (W16-S04: dmenu_run and dmenu_path
// are shell scripts), as Linux's binfmt_script: the first line, at most
// scriptLine bytes, names the interpreter and at most one argument (the
// rest of the line, spaces at its ends trimmed); the interpreter runs with
// argv = interpreter, the argument, the script's path as given, then the
// caller's argv after argv[0]. One level only: an interpreter that is a
// script itself is ENOEXEC (Linux allows a few). Docs: POSIX_COVERAGE.md.

import (
	"bytes"
	"io/fs"
	"strings"
	"syscall"
)

const scriptLine = 256 // Linux's BINPRM_BUF_SIZE

// script turns a "#!" file into its interpreter: the file to load, its
// contents and the new argv. Anything else comes back as it was.
func (p *Process) script(name, file string, data []byte, argv []string) (string, []byte, []string, error) {
	if !bytes.HasPrefix(data, []byte("#!")) {
		return file, data, argv, nil
	}
	line := data[2:min(len(data), scriptLine)]
	end := bytes.IndexByte(line, '\n')
	if end < 0 {
		return "", nil, nil, syscall.ENOEXEC
	}
	words := strings.TrimSpace(strings.ReplaceAll(string(line[:end]), "\t", " "))
	interp, arg, _ := strings.Cut(words, " ")
	if interp == "" {
		return "", nil, nil, syscall.ENOEXEC
	}
	ifile := p.resolve(interp)
	idata, err := fs.ReadFile(p.sys.files, ifile)
	switch {
	case err != nil:
		return "", nil, nil, err
	case bytes.HasPrefix(idata, []byte("#!")):
		return "", nil, nil, syscall.ENOEXEC
	}
	args := []string{interp}
	if arg = strings.TrimSpace(arg); arg != "" {
		args = append(args, arg)
	}
	args = append(args, name)
	if len(argv) > 1 {
		args = append(args, argv[1:]...)
	}
	return ifile, idata, args, nil
}
