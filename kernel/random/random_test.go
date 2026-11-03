package random

import (
	"bytes"
	"testing"
)

func TestSeedDecides(t *testing.T) {
	var seed [32]byte
	seed[0] = 7
	a, b := New(seed), New(seed)
	x, y := make([]byte, 37), make([]byte, 37) // not a multiple of 8
	a.Read(x)
	b.Read(y)
	if !bytes.Equal(x, y) {
		t.Fatal("one seed gave two streams")
	}
	seed[0] = 8
	b.Seed(seed)
	b.Read(y)
	a.Read(x)
	if bytes.Equal(x, y) {
		t.Fatal("a new seed changed nothing")
	}
}

func TestKernelLooksRandom(t *testing.T) {
	b := make([]byte, 4096)
	if n, err := Kernel.Read(b); n != len(b) || err != nil {
		t.Fatalf("Read = %d, %v", n, err)
	}
	var count [256]int
	for _, c := range b {
		count[c]++
	}
	for v, n := range count {
		if n > 64 { // 16 expected; a stuck generator fails this at once
			t.Fatalf("byte %#x appears %d times in 4096", v, n)
		}
	}
	if n, _ := Kernel.Write([]byte("entropy?")); n != 8 {
		t.Errorf("Write = %d", n)
	}
}
