package proc

import (
	"encoding/binary"
	"errors"
)

// The user stack: StackSize bytes ending at StackTop, mapped up front.
const (
	StackTop  = 0x7FFF_FFFF_0000
	StackSize = 256 << 10
)

// auxv types (Linux include/uapi/linux/auxvec.h)
const (
	atNull   = 0
	atPhdr   = 3
	atPhent  = 4
	atPhnum  = 5
	atPagesz = 6
	atEntry  = 9
	atUID    = 11
	atEUID   = 12
	atGID    = 13
	atEGID   = 14
	atSecure = 23
	atRandom = 25
	atExecfn = 31
)

// BuildStack writes the Linux entry stack below StackTop into m and returns
// the entry RSP (16-byte aligned, at argc): the Go twin of nucleus/go_stack.c.
//
//	[rsp] argc, argv..., NULL, envp..., NULL, auxv (type, value)..., AT_NULL
//	higher: the strings and 16 AT_RANDOM bytes
func BuildStack(m *Mem, img Image, argv, envp []string, random [16]byte) (uint64, error) {
	if len(argv) == 0 {
		return 0, errors.New("proc: argv needs at least the program name")
	}
	sp := uint64(StackTop)
	push := func(b []byte) (uint64, error) {
		sp -= uint64(len(b))
		return sp, m.CopyOut(sp, b)
	}
	str := func(s string) (uint64, error) { return push(append([]byte(s), 0)) }

	rnd, err := push(random[:])
	if err != nil {
		return 0, err
	}
	addrs := func(list []string) ([]uint64, error) {
		out := make([]uint64, len(list))
		for i, s := range list {
			var err error
			if out[i], err = str(s); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	av, err := addrs(argv)
	if err != nil {
		return 0, err
	}
	ev, err := addrs(envp)
	if err != nil {
		return 0, err
	}
	aux := []uint64{
		atPhdr, img.Phdr, atPhent, img.Phent, atPhnum, img.Phnum, atPagesz, PageSize,
		atEntry, img.Entry, atRandom, rnd, atExecfn, av[0],
		atUID, 0, atEUID, 0, atGID, 0, atEGID, 0, atSecure, 0, atNull, 0,
	}
	words := []uint64{uint64(len(av))}
	words = append(append(words, av...), 0)
	words = append(append(words, ev...), 0)
	words = append(words, aux...)
	sp = (sp - uint64(len(words))*8) &^ 15
	b := make([]byte, len(words)*8)
	for i, w := range words {
		binary.LittleEndian.PutUint64(b[i*8:], w)
	}
	return sp, m.CopyOut(sp, b)
}
