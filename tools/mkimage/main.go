// mkimage assembles the Burrow boot disk.
//
//	mkimage -o build/disk.img -stage1 build/stage1.bin -stage2 build/stage2.bin \
//	        -payload loader=build/loader.bin [-payload kernel=build/kernel.elf ...] [-video 1024x768x32]
//	mkimage -inspect build/disk.img
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

type payloadFlags []string

func (p *payloadFlags) String() string     { return strings.Join(*p, ",") }
func (p *payloadFlags) Set(v string) error { *p = append(*p, v); return nil }

func main() {
	out := flag.String("o", "", "output disk image")
	s1 := flag.String("stage1", "", "stage1 binary (512 bytes)")
	s2 := flag.String("stage2", "", "stage2 binary (<= 16 KiB)")
	minMiB := flag.Int("min-mib", 4, "pad image to at least this many MiB")
	inspect := flag.String("inspect", "", "print the manifest of an existing image and exit")
	video := flag.String("video", "", "WxHxB[,pitch]: a \"video\" payload asking stage2 for that VBE mode (W21-S01)")
	var payloads payloadFlags
	flag.Var(&payloads, "payload", "name=path (repeatable, order = disk order)")
	flag.Parse()

	if *inspect != "" {
		if err := doInspect(*inspect); err != nil {
			fail(err)
		}
		return
	}
	if *out == "" || *s1 == "" || *s2 == "" {
		flag.Usage()
		os.Exit(2)
	}
	stage1 := mustRead(*s1)
	stage2 := mustRead(*s2)
	var ps []Payload
	for _, spec := range payloads {
		name, path, ok := strings.Cut(spec, "=")
		if !ok {
			fail(fmt.Errorf("bad -payload %q, want name=path", spec))
		}
		ps = append(ps, Payload{Name: name, Data: mustRead(path)})
	}
	if *video != "" {
		v, err := ParseVideo(*video)
		if err != nil {
			fail(err)
		}
		b, _ := v.MarshalBinary()
		ps = append(ps, Payload{Name: VideoPayloadName, Data: b})
	}
	img, m, err := BuildImage(stage1, stage2, ps, *minMiB<<20)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(*out, img, 0o644); err != nil {
		fail(err)
	}
	printManifest(m)
	fmt.Printf("wrote %s (%d bytes)\n", *out, len(img))
}

func doInspect(path string) error {
	b := mustRead(path)
	if len(b) < 2*SectorSize {
		return fmt.Errorf("%s too small", path)
	}
	var m Manifest
	if err := m.UnmarshalBinary(b[SectorSize : 2*SectorSize]); err != nil {
		return err
	}
	printManifest(m)
	return nil
}

func printManifest(m Manifest) {
	fmt.Printf("%-16s %8s %8s %10s %8s\n", "NAME", "LBA", "SECTORS", "BYTES", "CRC32")
	for _, e := range m.Entries {
		fmt.Printf("%-16s %8d %8d %10d %08x\n", e.Name, e.LBA, e.Sectors, e.SizeBytes, e.CRC32)
	}
}

func mustRead(p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		fail(err)
	}
	return b
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "mkimage:", err)
	os.Exit(1)
}
