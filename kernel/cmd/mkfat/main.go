// Command mkfat builds a FAT32 disk image from a directory, and reads one
// back, with kernel/fs/fat. It runs on the host: the Makefile builds
// build/data.img (the kernel's second disk, mounted at /mnt) with it, and
// tests/regression/fat_interop.sh checks it against mkfs.fat and mtools.
//
//	go run ./cmd/mkfat -o ../build/data.img -label burrowdata ../datadisk
//	go run ./cmd/mkfat -ls image          list every file with its size
//	go run ./cmd/mkfat -cat image path    print one file
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"

	"burrow/kernel/fs/fat"
)

func main() {
	out := flag.String("o", "data.img", "image to write")
	label := flag.String("label", "BURROW", "volume label (11 characters at most)")
	sizeMiB := flag.Int("size", fat.MinImageBytes>>20, "image size in MiB")
	ls := flag.Bool("ls", false, "list the files of the image given as the argument")
	cat := flag.Bool("cat", false, "print file <path> of image <image>")
	flag.Parse()
	var err error
	switch {
	case *ls && flag.NArg() == 1:
		err = list(flag.Arg(0))
	case *cat && flag.NArg() == 2:
		err = show(flag.Arg(0), flag.Arg(1))
	case flag.NArg() == 1:
		err = build(*out, *label, *sizeMiB, flag.Arg(0))
	default:
		fmt.Fprintln(os.Stderr, "usage: mkfat [-o image] [-label name] [-size MiB] <directory>\n"+
			"       mkfat -ls <image>\n       mkfat -cat <image> <path>")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkfat:", err)
		os.Exit(1)
	}
}

func build(out, label string, sizeMiB int, dir string) error {
	img := make([]byte, sizeMiB<<20)
	if err := fat.Format(img, os.DirFS(dir), label); err != nil {
		return err
	}
	return os.WriteFile(out, img, 0o644)
}

func mount(image string) (*fat.FS, error) {
	f, err := os.Open(image)
	if err != nil {
		return nil, err
	}
	return fat.Mount(f) // the process exits soon: the file stays open
}

func list(image string) error {
	fsys, err := mount(image)
	if err != nil {
		return err
	}
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Printf("%s\t%d\n", p, info.Size())
		return nil
	})
}

func show(image, path string) error {
	fsys, err := mount(image)
	if err != nil {
		return err
	}
	b, err := fs.ReadFile(fsys, path)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(b)
	return err
}
