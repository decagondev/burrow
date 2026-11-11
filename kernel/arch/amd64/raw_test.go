package amd64

// raw_test.go - BUG-0034, checked on the host (the package itself builds
// only with the burrow tag): raw, through which every nucleus call that
// fills a buffer passes uintptr(unsafe.Pointer(&x[0])), must carry
// //go:uintptrescapes, so that such a buffer is on the heap for the call.
// Without it a stack array moved when the goroutine's stack grew, and the
// nucleus wrote the old copy: /dev/lattrace read zeros one boot in three.

import (
	"os"
	"strings"
	"testing"
)

func TestBUG0034RawKeepsBuffersOnTheHeap(t *testing.T) {
	src, err := os.ReadFile("user.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "//go:uintptrescapes\nfunc raw(nr uintptr, a ...uintptr)") {
		t.Fatal("raw lost its //go:uintptrescapes directive (BUG-0034)")
	}
}
