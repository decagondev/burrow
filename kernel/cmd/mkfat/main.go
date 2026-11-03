// Command mkfat builds a FAT32 disk image from a directory, and reads one
// back, with kernel/fs/fat. It runs on the host: the Makefile builds
// build/data.img (the kernel's second disk, mounted at /mnt) with it, and
// tests/regression/fat_interop.sh checks it against mkfs.fat and mtools.
//
//	go run ./cmd/mkfat -o ../build/data.img -label burrowdata ../datadisk
//	go run ./cmd/mkfat -ls image          list every file with its size
//	go run ./cmd/mkfat -cat image path    print one file
//	go run ./cmd/mkfat -put image path file / -mkdir image path / -rm image path
//	                                      change an image with the kernel's write code (W13-S04)
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"time"

	"burrow/kernel/fs/fat"
)

func main() {
	out := flag.String("o", "data.img", "image to write")
	label := flag.String("label", "BURROW", "volume label (11 characters at most)")
	sizeMiB := flag.Int("size", fat.MinImageBytes>>20, "image size in MiB")
	ls := flag.Bool("ls", false, "list the files of the image given as the argument")
	cat := flag.Bool("cat", false, "print file <path> of image <image>")
	put := flag.Bool("put", false, "write <file> into image <image> as <path> (W13-S04: the kernel's write code)")
	mkdir := flag.Bool("mkdir", false, "make directory <path> in image <image>")
	rm := flag.Bool("rm", false, "remove <path> (a file or an empty directory) from image <image>")
	flag.Parse()
	var err error
	switch {
	case *ls && flag.NArg() == 1:
		err = list(flag.Arg(0))
	case *cat && flag.NArg() == 2:
		err = show(flag.Arg(0), flag.Arg(1))
	case *put && flag.NArg() == 3:
		err = edit(flag.Arg(0), func(f *fat.FS) error {
			data, err := os.ReadFile(flag.Arg(2))
			if err != nil {
				return err
			}
			return f.WriteFile(flag.Arg(1), data, 0o644, time.Now())
		})
	case *mkdir && flag.NArg() == 2:
		err = edit(flag.Arg(0), func(f *fat.FS) error { return f.Mkdir(flag.Arg(1), 0o755, time.Now()) })
	case *rm && flag.NArg() == 2:
		err = edit(flag.Arg(0), func(f *fat.FS) error { return f.Remove(flag.Arg(1)) })
	case flag.NArg() == 1:
		err = build(*out, *label, *sizeMiB, flag.Arg(0))
	default:
		fmt.Fprintln(os.Stderr, "usage: mkfat [-o image] [-label name] [-size MiB] <directory>\n"+
			"       mkfat -ls <image>\n       mkfat -cat <image> <path>\n"+
			"       mkfat -put <image> <path> <file>\n       mkfat -mkdir <image> <path>\n       mkfat -rm <image> <path>")
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

// imageDisk is an image file as a fat.Disk; Flush syncs it.
type imageDisk struct{ *os.File }

func (d imageDisk) Flush() error { return d.Sync() }

// edit mounts image for writing and applies change to it.
func edit(image string, change func(*fat.FS) error) error {
	file, err := os.OpenFile(image, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	fsys, err := fat.MountRW(imageDisk{file})
	if err != nil {
		return err
	}
	return change(fsys)
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
